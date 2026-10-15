#!/usr/bin/env python3
"""Resolve and validate a manual release. Never change main or publish here."""
import json
import re
import subprocess
import tempfile
import sys
from pathlib import Path

from release import parse


def command(*args, missing_ok=False):
    result = subprocess.run(list(args), capture_output=True, text=True)
    if result.returncode:
        if missing_ok and "HTTP 404" in result.stderr:
            return None
        raise RuntimeError(result.stderr.strip() or f"Release-Prüfung fehlgeschlagen: {args[0]}")
    return result.stdout.strip()


def prepare(repository, tag, event_path, output_path, target_commit=""):
    _, suffix = parse(tag)
    if target_commit and not re.fullmatch(r"[0-9a-f]{40}", target_commit):
        raise ValueError("Ein vollständiger getesteter Commit-SHA wird benötigt")
    command("git", "fetch", "origin", "+refs/heads/main:refs/remotes/origin/main")
    commit = target_commit or command("git", "rev-parse", "origin/main")
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError("Main-Commit konnte nicht eindeutig aufgelöst werden")
    membership = subprocess.run(["git", "merge-base", "--is-ancestor", commit, "origin/main"],
                                capture_output=True, text=True)
    if membership.returncode:
        raise ValueError("Gewählter Commit liegt nicht auf main; zuerst PR manuell mergen")
    package = json.loads(command("git", "show", f"{commit}:package.json"))
    lock = json.loads(command("git", "show", f"{commit}:package-lock.json"))
    version = tag[1:]
    if package["version"] != version or lock.get("version") != version or lock.get("packages", {}).get("", {}).get("version") != version:
        raise ValueError(f"Release-Version {version} stimmt nicht mit package.json/package-lock.json des gewählten Commits überein. Version im Feature-Branch ändern, testen und manuell nach main mergen.")
    existing = command("gh", "api", f"repos/{repository}/releases/tags/{tag}", missing_ok=True)
    release = json.loads(existing) if existing is not None else {"tag_name": tag, "prerelease": bool(suffix)}
    if bool(release.get("prerelease")) != bool(suffix):
        raise ValueError("Vorabversionsstatus und Release-Tag müssen übereinstimmen")
    remote_tag = command("git", "ls-remote", "--tags", "origin", f"refs/tags/{tag}")
    if remote_tag:
        command("git", "fetch", "origin", f"refs/tags/{tag}:refs/tags/{tag}")
        if command("git", "rev-parse", f"{tag}^{{commit}}") != commit:
            raise ValueError("Release-Tag gehört bereits zu einem anderen Commit. Für eine Wiederholung dessen vollständigen SHA im Feld commit angeben.")
    elif existing is not None:
        if not release.get("draft") or release.get("target_commitish") != commit:
            raise ValueError("Vorhandenes Release/Entwurf hat keinen passenden Git-Tag oder Ziel-Commit")
    Path(event_path).write_text(json.dumps({"release": release}))
    with open(output_path, "a") as output:
        output.write(f"commit={commit}\ncreate_release={str(existing is None).lower()}\n")


def upload_assets(repository, tag, *paths):
    release = json.loads(command("gh", "api", f"repos/{repository}/releases/tags/{tag}"))
    existing = {asset["name"] for asset in release["assets"]}
    for item in paths:
        path = Path(item)
        if path.name in existing:
            with tempfile.TemporaryDirectory() as directory:
                command("gh", "release", "download", tag, "--repo", repository,
                        "--pattern", path.name, "--dir", directory)
                downloaded = Path(directory) / path.name
                matches = downloaded.read_bytes() == path.read_bytes()
                if path.name == "release-proof.json":
                    previous, current = json.loads(downloaded.read_text()), json.loads(path.read_text())
                    # A later full CI rerun can prove the same immutable image again.
                    # Keep the original release record when the actual artifacts match.
                    matches = all(previous.get(key) == current[key] for key in
                                  ("schema", "image", "version", "commit", "digest", "platforms", "release"))
                if not matches:
                    raise ValueError(f"Release-Asset {path.name} widerspricht dem geprüften Build; wird nicht überschrieben")
            print(f"Passendes Release-Asset wiederverwendet: {path.name}")
        else:
            command("gh", "release", "upload", tag, str(path), "--repo", repository)


if __name__ == "__main__":
    try:
        if sys.argv[1] == "assets":
            upload_assets(*sys.argv[2:])
        else:
            prepare(*sys.argv[1:])
    except (ValueError, RuntimeError, KeyError, OSError) as error:
        sys.exit(f"Release-Vorbereitung abgebrochen: {error}")
