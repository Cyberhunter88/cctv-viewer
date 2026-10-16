#!/usr/bin/env python3
"""Inspect, verify and copy tested OCI indexes. This module never builds images."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys

from release import parse, tags_for

ARCHES = ("amd64", "arm64")
DIGEST = re.compile(r"sha256:[0-9a-f]{64}\Z")


def docker(*args, missing_ok=False):
    result = subprocess.run(["docker", "buildx", "imagetools", *args],
                            capture_output=True, text=True)
    if result.returncode:
        # Authentication, rate limits and server errors must never mean "absent".
        if missing_ok and re.search(r"manifest unknown|not found|name unknown", result.stderr, re.I) and not re.search(r"unauthorized|denied|forbidden|429|timeout|certificate", result.stderr, re.I):
            return None
        raise RuntimeError(f"Registry-Anfrage fehlgeschlagen ({args[0]} {args[1]}): {result.stderr.strip()}")
    return result.stdout


def manifest(ref, missing_ok=False):
    raw = docker("inspect", ref, "--format", "{{json .Manifest}}", missing_ok=missing_ok)
    if raw is None:
        return None
    value = json.loads(raw)
    if not DIGEST.fullmatch(value.get("digest", "")):
        raise ValueError(f"Kein gültiger Image-Digest: {ref}")
    return value


def platforms(index):
    found = {}
    for child in index.get("manifests", []):
        platform = child.get("platform", {})
        arch = platform.get("architecture")
        if platform.get("os") == "unknown" and arch == "unknown":
            continue  # Preserve BuildKit provenance/SBOM descriptors when copying the index.
        if platform.get("os") != "linux" or arch not in ARCHES or arch in found:
            raise ValueError("Image enthält unerwartete oder doppelte Plattformen")
        digest = child.get("digest", "")
        if not DIGEST.fullmatch(digest):
            raise ValueError("Ungültiger Plattform-Digest")
        found[arch] = digest
    return found


def verify(image, index, version, commit, arches=ARCHES):
    found = platforms(index)
    if not set(arches).issubset(found):
        raise ValueError(f"Image enthält nicht alle verlangten Plattformen: {', '.join(arches)}")
    for arch in arches:
        config = json.loads(docker("inspect", f"{image}@{found[arch]}", "--format", "{{json .Image}}"))
        labels = config.get("config", {}).get("Labels", {})
        if config.get("os") != "linux" or config.get("architecture") != arch:
            raise ValueError(f"Plattform-Konfiguration für {arch} stimmt nicht")
        if labels.get("org.opencontainers.image.version") != version:
            raise ValueError(f"Image-Version für {arch} stimmt nicht mit package.json/Release {version} überein")
        if labels.get("org.opencontainers.image.revision") != commit:
            raise ValueError(f"Image-Commit für {arch} stimmt nicht mit {commit} überein")
    for child in index.get("manifests", []):
        if child.get("platform", {}).get("os") == "unknown":
            attestation = json.loads(docker("inspect", f"{image}@{child['digest']}", "--raw"))
            if not isinstance(attestation, dict):
                raise ValueError("Ungültiges SBOM/Provenienz-Manifest")
    return found


def write_output(**values):
    with open(os.environ["GITHUB_OUTPUT"], "a") as output:
        for key, value in values.items():
            output.write(f"{key}={value}\n")


def resolve(image, version, commit, arch):
    index = manifest(f"{image}:sha-{commit}", missing_ok=True)
    if index is None:
        write_output(exists="false")
        return
    if set(platforms(index)) != set(ARCHES):
        raise ValueError("Vorhandenes Commit-Image ist nicht vollständig; Tag manuell prüfen")
    verify(image, index, version, commit, (arch,))
    write_output(exists="true", ref=f"{image}@{index['digest']}")


def record(ref, version, commit, arch, path):
    image = ref.split("@", 1)[0]
    index = manifest(ref)
    found = verify(image, index, version, commit, (arch,))
    Path(path).write_text(json.dumps(dict(schema=1, image=image, commit=commit, version=version,
                                        arch=arch, source=index["digest"], platform=found[arch])))


def publish_commit(image, version, commit, directory, output):
    records = [json.loads((Path(directory) / f"{arch}.json").read_text()) for arch in ARCHES]
    expected, sources = {}, []
    for arch, item in zip(ARCHES, records):
        if any(item.get(key) != val for key, val in dict(schema=1, image=image, version=version,
                                                        commit=commit, arch=arch).items()):
            raise ValueError("Smoke-Test-Nachweis gehört zu einem anderen Commit, Image oder einer anderen Version")
        index = manifest(f"{image}@{item['source']}")
        found = verify(image, index, version, commit, (arch,))
        if found[arch] != item["platform"]:
            raise ValueError("Smoke-Test-Nachweis und Architektur-Digest stimmen nicht überein")
        expected[arch] = item["platform"]
        sources.append(f"{image}@{index['digest']}")
    target = f"{image}:sha-{commit}"
    existing = manifest(target, missing_ok=True)
    if existing is None:
        candidate = f"{image}:candidate-{os.environ['GITHUB_RUN_ID']}-{os.environ['GITHUB_RUN_ATTEMPT']}"
        docker("create", "-t", candidate, *dict.fromkeys(sources))
        existing = manifest(candidate)
        if verify(image, existing, version, commit) != expected:
            raise ValueError("Zusammengeführtes Image entspricht nicht den geprüften Architektur-Digests")
        docker("create", "-t", target, f"{image}@{existing['digest']}")
    actual = manifest(target)
    if actual["digest"] != existing["digest"] or verify(image, actual, version, commit) != expected:
        raise ValueError("Commit-Tag widerspricht den Smoke-Tests; vorhandenes Image wird nicht überschrieben")
    proof = dict(schema=1, image=image, version=version, commit=commit, digest=actual["digest"],
                 platforms=expected, run_id=int(os.environ["GITHUB_RUN_ID"]),
                 run_attempt=int(os.environ["GITHUB_RUN_ATTEMPT"]))
    Path(output).write_text(json.dumps(proof, indent=2) + "\n")
    summary(f"## Geprüftes Commit-Image\n`{target}`\n\nDigest: `{actual['digest']}`\n\n"
            + "\n".join(f"- linux/{arch}: `{digest}`" for arch, digest in expected.items()))


def checked_proof(image, version, commit, path):
    proof = json.loads(Path(path).read_text())
    source_image = proof.get("image")
    # Accept historical proofs from the release package as well as new build
    # proofs, but never take an arbitrary registry location from an artifact.
    if source_image not in (image, image + "-builds"):
        raise ValueError("Buildnachweis gehört nicht zum freigegebenen GHCR-Paket")
    expected = dict(schema=1, image=source_image, version=version, commit=commit,
                    run_id=int(os.environ["CI_RUN_ID"]), run_attempt=int(os.environ["CI_RUN_ATTEMPT"]))
    if any(proof.get(key) != value for key, value in expected.items()):
        raise ValueError("Buildnachweis gehört nicht zur freigegebenen Commit-CI/Version")
    index = manifest(f"{source_image}:sha-{commit}")
    if index["digest"] != proof.get("digest") or verify(source_image, index, version, commit) != proof.get("platforms"):
        raise ValueError("Commit-Image wurde verändert oder stimmt nicht mit den geprüften AMD64-/ARM64-Images überein")
    return proof


def promotion_plan(image, version, commit, proof_path, releases_path):
    parse("v" + version)
    proof = checked_proof(image, version, commit, proof_path)
    releases = [release for page in json.loads(Path(releases_path).read_text()) for release in page]
    tags = tags_for("v" + version, "-" in version, releases)
    current = manifest(f"{image}:{version}", missing_ok=True)
    if current is not None and current["digest"] != proof["digest"]:
        raise ValueError("Versions-Tag gehört bereits zu einem anderen Image; Veröffentlichung abgebrochen")
    allowed = [version]
    for tag in tags[1:]:
        alias = manifest(f"{image}:{tag}", missing_ok=True)
        if alias is None or alias["digest"] == proof["digest"]:
            allowed.append(tag)
            continue
        # Protect against partial newer releases and manual changes, too.
        children = platforms(alias)
        if set(children) != set(ARCHES):
            raise ValueError(f"Versionszeiger {tag} enthält kein bekanntes Multiarch-Image")
        config = json.loads(docker("inspect", f"{image}@{children['amd64']}", "--format", "{{json .Image}}"))
        previous = config.get("config", {}).get("Labels", {}).get("org.opencontainers.image.version", "")
        previous_version, pre = parse("v" + previous)
        owner = manifest(f"{image}:{previous}", missing_ok=True)
        if pre or owner is None or owner["digest"] != alias["digest"]:
            raise ValueError(f"Versionszeiger {tag} ist keinem stabilen Versions-Tag eindeutig zugeordnet")
        if previous_version == parse("v" + version)[0]:
            raise ValueError(f"Versionszeiger {tag} widerspricht dem geprüften Image derselben Version")
        if previous_version < parse("v" + version)[0]:
            allowed.append(tag)
    return proof, allowed


def copy_release(source, destination):
    # --all includes platform and BuildKit attestation manifests. Refuse format
    # conversion so the exact tested index and child digests remain unchanged.
    result = subprocess.run(["skopeo", "copy", "--all", "--preserve-digests",
                             "docker://" + source, "docker://" + destination],
                            capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError("Geprüftes Multiarch-Image konnte nicht unverändert kopiert werden: "
                           + result.stderr.strip())


def promote(image, version, commit, proof_path, releases_path, check_only=False):
    proof, tags = promotion_plan(image, version, commit, proof_path, releases_path)
    if check_only:
        print("Release-Vorabprüfung erfolgreich; keine Tags verändert.")
        return
    source = f"{proof['image']}@{proof['digest']}"
    ref = f"{image}@{proof['digest']}"
    # Stage the immutable version first. The workflow releases aliases only
    # after anonymous access and all release assets have been verified.
    published_tags = tags[:1] if os.environ.get("VERSION_ONLY") == "true" else tags
    for tag in published_tags:
        current = manifest(f"{image}:{tag}", missing_ok=True)
        if current is None or current["digest"] != proof["digest"]:
            if proof["image"] != image and tag == version:
                copy_release(source, f"{image}:{tag}")
            else:
                docker("create", "-t", f"{image}:{tag}", ref)
        actual = manifest(f"{image}:{tag}")
        if actual["digest"] != proof["digest"] or verify(image, actual, version, commit) != proof["platforms"]:
            raise ValueError(f"Digest/Plattformen von {tag} stimmen nicht mit dem geprüften Commit-Image überein")
    proof.update(source_image=proof["image"], image=image, release="v" + version, tags=tags)
    Path(os.environ["RELEASE_PROOF"]).write_text(json.dumps(proof, indent=2) + "\n")
    if os.environ.get("VERSION_ONLY") == "true":
        summary(f"## Release-Image v{version} vorbereitet\n\n"
                "Unveränderlicher Versionstag geprüft. Versions-Aliases bleiben bis nach "
                "Download-Prüfung und Asset-Upload unverändert.")
        return
    write_output(latest=str("latest" in tags).lower())
    summary(f"## Release v{version}\n\nCommit: `{commit}`\n\n"
            f"Geprüfte CI: {os.environ['GITHUB_SERVER_URL']}/{os.environ['GITHUB_REPOSITORY']}/actions/runs/{proof['run_id']}/attempts/{proof['run_attempt']}\n\n"
            f"Build-Quelle: `{source}`\n\nImage: `{ref}`\n\nTags: " + ", ".join(f"`{tag}`" for tag in tags) + "\n\n"
            + "\n".join(f"- linux/{arch}: `{digest}`" for arch, digest in proof['platforms'].items())
            + "\n\nVersionszeiger werden nur vorwärts gesetzt. `release-proof.json` dokumentiert die Zuordnung.")


def summary(message):
    with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as output:
        output.write(message + "\n")


if __name__ == "__main__":
    try:
        operation, *arguments = sys.argv[1:]
        if operation == "resolve":
            resolve(*arguments)
        elif operation == "record":
            record(*arguments)
        elif operation == "publish-commit":
            publish_commit(*arguments)
        elif operation == "verify":
            checked_proof(*arguments)
        elif operation in ("promote", "preflight"):
            promote(*arguments, check_only=operation == "preflight")
        else:
            raise ValueError("Unbekannte Registry-Operation")
    except (ValueError, RuntimeError, KeyError, OSError) as error:
        sys.exit(f"Image-Prüfung fehlgeschlagen: {error}")
