#!/usr/bin/env python3
"""Resolve a manually requested release without publishing before CI passes."""
import json
import subprocess
import sys
from pathlib import Path

from release import parse


def command(*args, missing_ok=False):
    result = subprocess.run(list(args), capture_output=True, text=True)
    if result.returncode:
        if missing_ok and "HTTP 404" in result.stderr:
            return None
        raise RuntimeError(result.stderr.strip() or "Release-Vorbereitung fehlgeschlagen")
    return result.stdout.strip()


def prepare(repository, tag, event_path, output_path):
    if not tag:
        tag = "v" + json.loads(Path("package.json").read_text())["version"]
    _, suffix = parse(tag)
    existing = command("gh", "api", f"repos/{repository}/releases/tags/{tag}", missing_ok=True)
    if existing is None:
        release = {"tag_name": tag, "prerelease": bool(suffix)}
    else:
        release = json.loads(existing)
        if release["draft"]:
            raise ValueError("Release existiert als Entwurf; bitte zuerst manuell klären")
        if bool(release["prerelease"]) != bool(suffix):
            raise ValueError("Vorabversionsstatus und Release-Tag müssen übereinstimmen")
    # Resolve an existing tag, including annotated tags, to its exact commit.
    remote_tag = command("git", "ls-remote", "--tags", "origin", f"refs/tags/{tag}")
    if remote_tag:
        command("git", "fetch", "origin", f"refs/tags/{tag}:refs/tags/{tag}")
        commit = command("git", "rev-parse", f"{tag}^{{commit}}")
    else:
        if existing is not None:
            raise ValueError("Veröffentlichtes Release hat keinen Git-Tag")
        commit = command("git", "rev-parse", "HEAD")
    Path(event_path).write_text(json.dumps({"release": release}))
    with open(output_path, "a") as output:
        output.write(f"commit={commit}\ncreate_release={str(existing is None).lower()}\n")


if __name__ == "__main__":
    try:
        prepare(*sys.argv[1:])
    except (ValueError, RuntimeError) as error:
        sys.exit(str(error))
