#!/usr/bin/env python3
"""Dispatch release publication for a successfully built main commit."""
import json
import re
import subprocess
import sys
from pathlib import Path

from release import parse


def gh(*args, missing_ok=False):
    result = subprocess.run(["gh", *args], capture_output=True, text=True)
    if result.returncode:
        if missing_ok and "HTTP 404" in result.stderr:
            return None
        raise RuntimeError(result.stderr.strip() or "GitHub-Anfrage fehlgeschlagen")
    return result.stdout


def dispatch(repository, commit, version):
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError("Ein vollständiger getesteter Commit-SHA wird benötigt")
    tag = "v" + version
    parse(tag)
    existing = gh("api", f"repos/{repository}/releases/tags/{tag}", missing_ok=True)
    if existing is not None:
        if json.loads(existing)["draft"]:
            raise ValueError(f"{tag} existiert als Entwurf; bitte zuerst manuell klären")
        released_commit = json.loads(gh("api", f"repos/{repository}/commits/{tag}"))["sha"]
        if released_commit != commit:
            print(f"{tag} ist bereits veröffentlicht. Für das nächste Release package.json erhöhen.")
            return False
    # GITHUB_TOKEN supports workflow_dispatch; release events created by it
    # would not start another workflow. Pass the immutable tested commit too.
    gh("workflow", "run", "release.yml", "--repo", repository, "--ref", "main",
       "-f", f"tag={tag}", "-f", f"commit={commit}")
    print(f"Release-Veröffentlichung für {tag} ({commit}) gestartet.")
    return True


if __name__ == "__main__":
    try:
        version = json.loads(Path("package.json").read_text())["version"]
        dispatch(sys.argv[1], sys.argv[2], version)
    except (ValueError, RuntimeError) as error:
        sys.exit(str(error))
