#!/usr/bin/env python3
"""Wait for the push CI of the exact release commit, including image publication."""
import json
import re
import subprocess
import sys
import time


def wait_for_ci(repository, commit, timeout=1800, interval=15):
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError("Ein vollständiger Commit-SHA wird benötigt")
    started = time.monotonic()
    deadline = started + timeout
    while time.monotonic() < deadline:
        result = subprocess.run(
            ["gh", "api", f"repos/{repository}/actions/workflows/ci.yml/runs"
             f"?event=push&head_sha={commit}&per_page=100"],
            check=True, capture_output=True, text=True,
        )
        runs = [run for run in json.loads(result.stdout)["workflow_runs"]
                if run["head_sha"] == commit and run["event"] == "push"]
        if runs:
            run = max(runs, key=lambda item: item["id"])
            if run["status"] == "completed":
                if run["conclusion"] != "success":
                    raise RuntimeError(f"Commit-CI ist {run['conclusion']}: {run['html_url']}")
                print(f"Commit-CI erfolgreich: {run['html_url']}", flush=True)
                return
            print(f"Warte auf Commit-CI: {run['html_url']}", flush=True)
        elif time.monotonic() - started >= 90:
            raise RuntimeError("Keine Push-CI für diesen Commit gefunden. Commit auf einen "
                               "Branch pushen und die CI mit Image-Veröffentlichung ausführen.")
        time.sleep(min(interval, max(0, deadline - time.monotonic())))
    raise TimeoutError("Commit-CI nach 30 Minuten noch nicht abgeschlossen. "
                       "Release-Workflow nach erfolgreicher CI erneut starten.")


if __name__ == "__main__":
    try:
        wait_for_ci(*sys.argv[1:])
    except (ValueError, RuntimeError, TimeoutError, subprocess.CalledProcessError) as error:
        sys.exit(str(error))
