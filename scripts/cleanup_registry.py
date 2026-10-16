#!/usr/bin/env python3
"""Preview or explicitly delete old, unreferenced GHCR build-wrapper versions."""
import argparse
from datetime import datetime, timedelta, timezone
import json
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import quote

DIGEST = re.compile(r"sha256:[0-9a-f]{64}\Z")
TEMPORARY = re.compile(r"(?:build-[0-9]+-[0-9]+-(?:amd64|arm64)|candidate-[0-9]+-[0-9]+)\Z")


class ManifestMissing(RuntimeError):
    """The registry explicitly reports MANIFEST_UNKNOWN for a digest."""


def command(*args):
    result = subprocess.run(args, capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError(f"{args[0]} fehlgeschlagen: {result.stderr.strip()}")
    return result.stdout


def api(path, method="GET"):
    args = ["gh", "api", "--method", method]
    if method == "GET":
        args += ["--paginate", "--slurp"]
    text = command(*args, path)
    return json.loads(text) if text.strip() else None


def registry_manifest(image, digest):
    result = subprocess.run(
        ["skopeo", "inspect", "--raw", "docker://" + image + "@" + digest],
        capture_output=True, text=True)
    if result.returncode:
        # Do not confuse authentication, transport or parsing errors with absence.
        if re.search(r": manifest unknown(?:\s|[\"\n]|$)", result.stderr):
            raise ManifestMissing(digest)
        raise RuntimeError(f"skopeo fehlgeschlagen: {result.stderr.strip()}")
    value = json.loads(result.stdout)
    if not isinstance(value, dict):
        raise ValueError("Ungültiges Registry-Manifest")
    return value


def references(manifest):
    media_type = manifest.get("mediaType")
    indexes = {"application/vnd.oci.image.index.v1+json", "application/vnd.docker.distribution.manifest.list.v2+json"}
    images = {"application/vnd.oci.image.manifest.v1+json", "application/vnd.docker.distribution.manifest.v2+json"}
    if manifest.get("schemaVersion") != 2 or media_type not in indexes | images:
        raise ValueError("Unbekannter Manifesttyp; Bereinigung wird abgebrochen")
    descriptors = manifest.get("manifests") if media_type in indexes else []
    if not isinstance(descriptors, list):
        raise ValueError("Ungültige Manifest-Referenzen")
    descriptors = list(descriptors)
    if "subject" in manifest:
        descriptors.append(manifest["subject"])
    annotation = manifest.get("annotations", {}).get("vnd.docker.reference.digest")
    if annotation:
        descriptors.append({"digest": annotation})
    result = set()
    for descriptor in descriptors:
        if not isinstance(descriptor, dict):
            raise ValueError("Ungültiger Manifest-Deskriptor")
        digest = descriptor.get("digest", "")
        if not isinstance(digest, str) or not DIGEST.fullmatch(digest):
            raise ValueError("Ungültiger referenzierter Digest")
        result.add(digest)
        reference = descriptor.get("annotations", {}).get("vnd.docker.reference.digest")
        if reference:
            if not isinstance(reference, str) or not DIGEST.fullmatch(reference):
                raise ValueError("Ungültige Attestierungs-Referenz")
            result.add(reference)
    return result


def timestamp(value):
    date = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if date.tzinfo is None:
        raise ValueError("Zeitstempel ohne Zeitzone")
    return date


def plan(versions, image, fetch=None, now=None, min_age_days=14, selection=None):
    if min_age_days < 1:
        raise ValueError("Mindestens ein Tag Sicherheitsabstand erforderlich")
    fetch = fetch or registry_manifest
    now = now or datetime.now(timezone.utc)
    cutoff = now - timedelta(days=min_age_days)
    by_digest, eligible = {}, set()
    for version in versions:
        digest, identifier = version["name"], version["id"]
        tags = version["metadata"]["container"]["tags"]
        if (not DIGEST.fullmatch(digest) or type(identifier) is not int or identifier <= 0
                or digest in by_digest or not isinstance(tags, list)
                or not all(isinstance(tag, str) for tag in tags)):
            raise ValueError("Ungültige oder doppelte GHCR-Version")
        by_digest[digest] = version
        modified = max(timestamp(version["created_at"]), timestamp(version["updated_at"]))
        # Any additional tag, including sha-*, SemVer, aliases or unknown tags,
        # makes the entire GHCR version a protected root. Untagged versions too.
        if tags and all(TEMPORARY.fullmatch(tag) for tag in tags) and modified < cutoff:
            eligible.add(digest)
    if len({v["id"] for v in versions}) != len(versions):
        raise ValueError("Doppelte GHCR-Versions-ID")
    if selection is not None:
        eligible = {digest for digest in eligible if by_digest[digest]["id"] in selection}
    visited, pending, missing = set(), list(set(by_digest) - eligible), set()
    while pending:
        digest = pending.pop()
        if digest in visited:
            continue
        visited.add(digest)
        try:
            manifest = fetch(image, digest)
        except ManifestMissing:
            # A dangling subject/attestation reference outside the package
            # inventory cannot be deleted by this cleanup. Inventory entries
            # must remain readable so their dependencies can be protected.
            if digest in by_digest:
                raise
            missing.add(digest)
            continue
        pending.extend(references(manifest) - visited)
    candidates = [dict(id=v["id"], digest=digest, tags=v["metadata"]["container"]["tags"])
                  for digest, v in by_digest.items() if digest in eligible - visited]
    return dict(schema=1, image=image, min_age_days=min_age_days,
                candidates=sorted(candidates, key=lambda item: item["id"]),
                retained=len(versions) - len(candidates),
                missing_external_manifests=sorted(missing))


def inventory(repository):
    pages = api("repos/" + repository)
    owner_type = pages[0]["owner"]["type"]
    if owner_type not in ("User", "Organization"):
        raise ValueError("Unbekannter Repository-Eigentümer")
    owner, package = repository.split("/")
    endpoint = ("users/" if owner_type == "User" else "orgs/") + owner + "/packages/container/" + quote(package, safe="") + "/versions"
    versions = [item for page in api(endpoint + "?per_page=100") for item in page]
    return endpoint, versions


def assert_idle(repository):
    for workflow in ("ci.yml", "release.yml"):
        for status in ("queued", "in_progress", "waiting", "pending", "requested"):
            pages = api(f"repos/{repository}/actions/workflows/{workflow}/runs?status={status}&per_page=100")
            if any(page["workflow_runs"] for page in pages):
                raise ValueError("CI oder Release läuft/wartet; Bereinigung später erneut starten")


def fingerprint(versions):
    return json.dumps(sorted(versions, key=lambda v: v["id"]), sort_keys=True)


def cleanup(repository, output, selected=(), min_age_days=14):
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        raise ValueError("Ungültiges Repository")
    image = "ghcr.io/" + repository.lower()
    endpoint, versions = inventory(repository)
    proposal = plan(versions, image, min_age_days=min_age_days)
    Path(output).write_text(json.dumps(proposal, indent=2) + "\n")
    selected = set(selected)
    if not selected:
        print(f"Vorschau: {len(proposal['candidates'])} löschbare interne Versionen; {proposal['retained']} geschützt. Plan: {output}")
        return
    allowed = {item["id"] for item in proposal["candidates"]}
    if not selected.issubset(allowed):
        raise ValueError("Auswahl enthält geschützte, zu junge oder unbekannte Versionen; nichts gelöscht")
    selected_plan = plan(versions, image, min_age_days=min_age_days, selection=selected)
    if selected != {item["id"] for item in selected_plan["candidates"]}:
        raise ValueError("Auswahl wird noch von beibehaltenen Manifesten referenziert; nichts gelöscht")
    assert_idle(repository)
    # A new image, alias change or newly added manifest reference invalidates
    # the reviewed inventory. Never delete from a stale snapshot.
    _, current = inventory(repository)
    if fingerprint(current) != fingerprint(versions):
        raise ValueError("Registry-Inventar hat sich verändert; neue Vorschau erstellen")
    for identifier in sorted(selected):
        assert_idle(repository)
        api(endpoint + "/" + str(identifier), method="DELETE")
        print(f"Unreferenzierte interne Version gelöscht: {identifier}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--min-age-days", type=int, default=14)
    parser.add_argument("--delete-version-id", type=int, action="append", default=[])
    args = parser.parse_args()
    cleanup(args.repository, args.output, args.delete_version_id, args.min_age_days)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, RuntimeError, KeyError, TypeError, OSError) as error:
        sys.exit(f"GHCR-Bereinigung abgebrochen: {error}")
