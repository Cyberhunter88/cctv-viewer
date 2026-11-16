"""Housekeeping keeps recent evidence and current main caches and deletes only with --apply."""
from datetime import datetime, timedelta, timezone
import unittest
from unittest.mock import patch

from housekeeping import housekeeping, select_caches, select_runs

NOW = datetime(2026, 11, 5, tzinfo=timezone.utc)


def run(number, workflow=1, days=40, conclusion="success", status="completed"):
    return dict(id=number, workflow_id=workflow, status=status, conclusion=conclusion,
                updated_at=(NOW - timedelta(days=days)).isoformat())


def cache(number, ref, days, key="buildkit-blob-1-sha256:abc", created=None):
    return dict(id=number, ref=ref, key=key, size_in_bytes=1024 ** 2,
                last_accessed_at=(NOW - timedelta(days=days)).isoformat(),
                created_at=(NOW - timedelta(days=days if created is None else created)).isoformat())


class HousekeepingTest(unittest.TestCase):
    def test_runs_keep_recent_active_current_and_newest_successes(self):
        runs = [run(i) for i in range(1, 13)]  # twelve old successes
        runs += [run(20, days=5), run(21, conclusion="failure"), run(22, status="in_progress", conclusion=None),
                 run(23, conclusion="cancelled"), run(30, workflow=2, conclusion="failure")]
        selected = select_runs(runs, NOW, current_run_id=23)
        self.assertEqual(selected, [1, 2, 3, 21, 30])

    def test_caches_spare_main_and_recent_entries(self):
        caches = [cache(1, "refs/heads/main", 90), cache(2, "refs/pull/7/merge", 3),
                  cache(3, "refs/pull/7/merge", 1), cache(4, "refs/heads/codex/x", 8),
                  cache(5, "refs/heads/codex/x", 3)]
        self.assertEqual(select_caches(caches, NOW), [2, 4])

    def test_caches_of_closed_pulls_go_at_once(self):
        caches = [cache(1, "refs/pull/7/merge", 0), cache(2, "refs/pull/8/merge", 0), cache(3, "refs/heads/main", 0)]
        self.assertEqual(select_caches(caches, NOW, open_pulls={8}), [1])

    def test_superseded_tool_caches_go_but_newest_and_buildkit_stay(self):
        go = "setup-go-Linux-x64-ubuntu24-go-1.27.3-"
        caches = [cache(1, "refs/heads/main", 0, key=go + "old", created=5),
                  cache(2, "refs/heads/main", 0, key=go + "new", created=1),
                  cache(3, "refs/heads/main", 0, key="node-cache-Linux-x64-npm-aaa", created=3),
                  cache(4, "refs/heads/main", 0, key="buildkit-blob-1-sha256:a", created=9),
                  cache(5, "refs/heads/main", 0, key="buildkit-blob-1-sha256:b", created=1),
                  cache(6, "refs/heads/main", 0, key="index-commit-amd64", created=9),
                  cache(7, "refs/pull/8/merge", 0, key="playwright-webkit-Linux-1.64.0", created=2),
                  cache(8, "refs/pull/8/merge", 0, key="playwright-webkit-Linux-1.64.1", created=1),
                  cache(9, "refs/pull/9/merge", 0, key="playwright-webkit-Linux-1.64.0", created=2)]
        self.assertEqual(select_caches(caches, NOW, open_pulls={8, 9}), [1, 7])

    def test_preview_never_deletes_and_apply_does(self):
        responses = [[dict(workflow_runs=[run(2, conclusion="failure")])],
                     [dict(actions_caches=[cache(9, "refs/pull/1/merge", 5)])],
                     [[dict(number=1)]]]
        with patch("housekeeping.api", side_effect=responses) as api:
            housekeeping("example/viewer", now=NOW)
            self.assertEqual(api.call_count, 3)
        with patch("housekeeping.api", side_effect=[*responses, None, None]) as api:
            self.assertEqual(housekeeping("example/viewer", apply=True, now=NOW), ([2], [9]))
            self.assertEqual([c.kwargs.get("method") for c in api.call_args_list[3:]], ["DELETE", "DELETE"])

    def test_invalid_repository_is_rejected(self):
        with self.assertRaises(ValueError):
            housekeeping("bad repo; rm")


if __name__ == "__main__":
    unittest.main()
