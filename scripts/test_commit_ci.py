import json
import subprocess
import unittest
from unittest.mock import patch

from wait_for_commit_ci import wait_for_ci

SHA = "a" * 40


def run(status="completed", conclusion="success", **kwargs):
    return dict(id=42, event="push", head_branch="main", head_sha=SHA, status=status,
                conclusion=conclusion, html_url="https://github.com/example/repo/actions/runs/42",
                **kwargs)


def response(runs):
    return subprocess.CompletedProcess([], 0, stdout=json.dumps({"workflow_runs": runs}))


class CommitCITest(unittest.TestCase):
    @patch("wait_for_commit_ci.time.sleep")
    @patch("wait_for_commit_ci.subprocess.run")
    def test_waits_for_exact_commit_push_and_publication(self, gh, sleep):
        other = run()
        other["head_sha"] = "b" * 40
        pr = run()
        pr["event"] = "pull_request"
        branch = run()
        branch["head_branch"] = "feature/example"
        gh.side_effect = [response([other, pr, branch]), response([run("in_progress", None)]),
                          response([run()])]
        wait_for_ci("example/repo", SHA)
        self.assertEqual(gh.call_count, 3)
        self.assertEqual(sleep.call_count, 2)
        self.assertIn(f"head_sha={SHA}", gh.call_args.args[0][2])
        self.assertIn("branch=main", gh.call_args.args[0][2])

    @patch("wait_for_commit_ci.subprocess.run")
    def test_failed_or_cancelled_ci_blocks_release(self, gh):
        for conclusion in ("failure", "cancelled", "timed_out", "skipped"):
            with self.subTest(conclusion=conclusion):
                gh.return_value = response([run(conclusion=conclusion)])
                with self.assertRaisesRegex(RuntimeError, conclusion):
                    wait_for_ci("example/repo", SHA)

    @patch("wait_for_commit_ci.subprocess.run")
    def test_latest_run_must_pass(self, gh):
        old = run()
        old["id"] = 1
        gh.return_value = response([old, run(conclusion="failure")])
        with self.assertRaises(RuntimeError):
            wait_for_ci("example/repo", SHA)

    @patch("wait_for_commit_ci.time.monotonic", side_effect=[0, 0, 91])
    @patch("wait_for_commit_ci.subprocess.run", return_value=response([]))
    def test_missing_ci_has_actionable_error(self, gh, monotonic):
        with self.assertRaisesRegex(RuntimeError, "nach main"):
            wait_for_ci("example/repo", SHA)

    @patch("wait_for_commit_ci.time.sleep")
    @patch("wait_for_commit_ci.time.monotonic", side_effect=[0, 0, 1, 2])
    @patch("wait_for_commit_ci.subprocess.run", return_value=response([run("queued", None)]))
    def test_running_ci_times_out(self, gh, monotonic, sleep):
        with self.assertRaises(TimeoutError):
            wait_for_ci("example/repo", SHA, timeout=2)

    @patch("wait_for_commit_ci.subprocess.run", side_effect=subprocess.CalledProcessError(1, "gh"))
    def test_api_failure_is_not_ignored(self, gh):
        with self.assertRaises(subprocess.CalledProcessError):
            wait_for_ci("example/repo", SHA)

    def test_rejects_non_sha_ref(self):
        with self.assertRaises(ValueError):
            wait_for_ci("example/repo", "main")


if __name__ == "__main__":
    unittest.main()
