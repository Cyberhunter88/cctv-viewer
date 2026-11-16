#!/usr/bin/env python3
"""Remove obsolete GitHub Actions runs and caches; preview unless --apply is given."""
import argparse
from datetime import datetime, timedelta, timezone
import json
import os
import re
import subprocess
import sys

MAX_DELETIONS = 300


def api(path, method="GET"):
    args = ["gh", "api", "--method", method]
    if method == "GET":
        args += ["--paginate", "--slurp"]
    result = subprocess.run([*args, path], capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError(f"gh fehlgeschlagen: {result.stderr.strip()}")
    return json.loads(result.stdout) if result.stdout.strip() else None


def timestamp(value):
    date = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if date.tzinfo is None:
        raise ValueError("Zeitstempel ohne Zeitzone")
    return date


def select_runs(runs, now, keep_days=30, keep_successful=10, current_run_id=None):
    """Old finished runs; the newest successful runs of each workflow always stay."""
    cutoff = now - timedelta(days=keep_days)
    protected = set()
    by_workflow = {}
    for run in runs:
        by_workflow.setdefault(run["workflow_id"], []).append(run)
    for group in by_workflow.values():
        successes = sorted((r for r in group if r["conclusion"] == "success"), key=lambda r: r["id"], reverse=True)
        protected.update(r["id"] for r in successes[:keep_successful])
    return sorted(r["id"] for r in runs
                  if r["status"] == "completed" and r["id"] not in protected
                  and r["id"] != current_run_id and timestamp(r["updated_at"]) < cutoff)


# Tool caches whose key ends in a version or hash. A newer entry of the same family
# on the same ref replaces the older ones. BuildKit blobs share no such family.
VERSIONED_CACHE = re.compile(r"((?:setup-go|node-cache|playwright|actionlint)-.+)-[^-]+")
PULL_REF = re.compile(r"refs/pull/([0-9]+)/(?:merge|head)")


def superseded_caches(caches):
    """Older entries of a versioned tool cache that a newer entry on the same ref replaced."""
    newest = {}
    result = []
    for cache in sorted(caches, key=lambda c: (timestamp(c["created_at"]), c["id"]), reverse=True):
        match = VERSIONED_CACHE.fullmatch(cache["key"])
        if not match:
            continue
        family = (cache["ref"], match.group(1))
        if family in newest:
            result.append(cache["id"])
        else:
            newest[family] = cache["id"]
    return result


def select_caches(caches, now, open_pulls=None, pull_days=2, branch_days=7):
    """Stale or closed PR/branch caches and superseded tool caches; current main caches stay."""
    result = set(superseded_caches(caches))
    for cache in caches:
        ref, accessed = cache["ref"], timestamp(cache["last_accessed_at"])
        if ref == "refs/heads/main":
            continue
        pull = PULL_REF.fullmatch(ref)
        if pull and open_pulls is not None and int(pull.group(1)) not in open_pulls:
            result.add(cache["id"])
            continue
        days = pull_days if pull else branch_days
        if accessed < now - timedelta(days=days):
            result.add(cache["id"])
    return sorted(result)


def housekeeping(repository, apply=False, now=None, keep_days=30, current_run_id=None):
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        raise ValueError("Ungültiges Repository")
    now = now or datetime.now(timezone.utc)
    runs = [r for page in api(f"repos/{repository}/actions/runs?status=completed&per_page=100")
            for r in page["workflow_runs"]]
    caches = [c for page in api(f"repos/{repository}/actions/caches?per_page=100")
              for c in page["actions_caches"]]
    open_pulls = {p["number"] for page in api(f"repos/{repository}/pulls?state=open&per_page=100") for p in page}
    old_runs = select_runs(runs, now, keep_days, current_run_id=current_run_id)[:MAX_DELETIONS]
    old_caches = select_caches(caches, now, open_pulls)[:MAX_DELETIONS]
    selected = set(old_caches)
    freed = sum(c.get("size_in_bytes", 0) for c in caches if c["id"] in selected) / 1024 ** 2
    lines = [f"## Aufräumen\n\nAlte Workflow-Läufe: {len(old_runs)} · Caches: {len(old_caches)} "
             f"({freed:.0f} MiB) · Modus: {'Löschen' if apply else 'Vorschau'}\n"]
    if apply:
        for identifier in old_runs:
            api(f"repos/{repository}/actions/runs/{identifier}", method="DELETE")
        for identifier in old_caches:
            api(f"repos/{repository}/actions/caches/{identifier}", method="DELETE")
    print(lines[0])
    if os.environ.get("GITHUB_STEP_SUMMARY"):
        with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as summary:
            summary.write(lines[0])
    return old_runs, old_caches


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--keep-days", type=int, default=30)
    parser.add_argument("--current-run-id", type=int)
    args = parser.parse_args()
    if args.keep_days < 7:
        parser.error("--keep-days mindestens 7")
    housekeeping(args.repository, args.apply, keep_days=args.keep_days, current_run_id=args.current_run_id)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, RuntimeError, KeyError, TypeError, OSError) as error:
        sys.exit(f"Aufräumen abgebrochen: {error}")
