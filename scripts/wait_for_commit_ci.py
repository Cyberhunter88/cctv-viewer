#!/usr/bin/env python3
"""Wait for the main-branch push CI of the exact release commit, including image publication."""
import json
import re
import subprocess
import sys
import time


REQUIRED_JOBS = {"Code prüfen und Testserver bauen", "Browser prüfen (chrome)",
                 "Browser prüfen (webkit)", "Docker prüfen (amd64)", "Docker prüfen (arm64)",
                 "Commit-Image bereitstellen", "Anwendung prüfen"}


def verify_jobs(repository, run):
    attempt = run["run_attempt"]
    result = subprocess.run(["gh", "api", "--paginate", "--slurp",
                             f"repos/{repository}/actions/runs/{run['id']}/attempts/{attempt}/jobs?per_page=100"],
                            check=True, capture_output=True, text=True)
    jobs = [job for page in json.loads(result.stdout) for job in page["jobs"]]
    passed = {job["name"] for job in jobs if job["conclusion"] == "success"}
    if not REQUIRED_JOBS.issubset(passed) or any(job["conclusion"] != "success" for job in jobs):
        raise RuntimeError("Commit-CI ist unvollständig: Code, beide Browser, beide Docker-Smoke-Tests, "
                           "Commit-Image und Abschlussprüfung müssen erfolgreich sein. " + run["html_url"])


def wait_for_ci(repository, commit, timeout=1800, interval=15, output_path=None):
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError("Ein vollständiger Commit-SHA wird benötigt")
    started = time.monotonic()
    deadline = started + timeout
    while time.monotonic() < deadline:
        result = subprocess.run(
            ["gh", "api", f"repos/{repository}/actions/workflows/ci.yml/runs"
             f"?event=push&branch=main&head_sha={commit}&per_page=100"],
            check=True, capture_output=True, text=True,
        )
        runs = [run for run in json.loads(result.stdout)["workflow_runs"]
                if run["head_sha"] == commit and run["event"] == "push"
                and run["head_branch"] == "main"]
        if runs:
            run = max(runs, key=lambda item: item["id"])
            if run["status"] == "completed":
                if run["conclusion"] != "success":
                    raise RuntimeError(f"Commit-CI ist {run['conclusion']}: {run['html_url']}")
                verify_jobs(repository, run)
                if output_path:
                    with open(output_path, "a") as output:
                        output.write(f"ci_run_id={run['id']}\nci_run_attempt={run['run_attempt']}\n")
                print(f"Commit-CI erfolgreich: {run['html_url']}", flush=True)
                return
            print(f"Warte auf Commit-CI: {run['html_url']}", flush=True)
        elif time.monotonic() - started >= 90:
            raise RuntimeError("Keine Push-CI auf main für diesen Commit gefunden. Commit nach main "
                               "mergen und die CI mit Image-Veröffentlichung ausführen.")
        time.sleep(min(interval, max(0, deadline - time.monotonic())))
    raise TimeoutError("Commit-CI nach 30 Minuten noch nicht abgeschlossen. "
                       "Release-Workflow nach erfolgreicher CI erneut starten.")


if __name__ == "__main__":
    try:
        wait_for_ci(sys.argv[1], sys.argv[2], output_path=sys.argv[3] if len(sys.argv) > 3 else None)
    except (ValueError, RuntimeError, TimeoutError, subprocess.CalledProcessError) as error:
        detail = error.stderr.strip() if isinstance(error, subprocess.CalledProcessError) and error.stderr else str(error)
        sys.exit("Commit-CI-Prüfung abgebrochen: " + detail)
