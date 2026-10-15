import json
import subprocess
import unittest
from unittest.mock import patch

from wait_for_commit_ci import wait_for_ci, REQUIRED_JOBS

SHA = "a" * 40


def run(status="completed", conclusion="success", **kwargs):
    return dict(id=42, run_attempt=1, event="push", head_branch="main", head_sha=SHA, status=status,
                conclusion=conclusion, html_url="https://github.com/example/repo/actions/runs/42",
                **kwargs)


def jobs():
    return subprocess.CompletedProcess([], 0, stdout=json.dumps([{"jobs": [{"name": name, "conclusion": "success"} for name in REQUIRED_JOBS]}]))


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
                          response([run()]), jobs()]
        wait_for_ci("example/repo", SHA)
        self.assertEqual(gh.call_count, 4)
        self.assertEqual(sleep.call_count, 2)
        self.assertIn(f"head_sha={SHA}", gh.call_args_list[0].args[0][2])
        self.assertIn("branch=main", gh.call_args_list[0].args[0][2])

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

    @patch("wait_for_commit_ci.subprocess.run")
    def test_green_workflow_with_skipped_or_missing_build_is_not_enough(self, gh):
        for failed in ["Docker prüfen (arm64)", "Browser prüfen (webkit)", "Commit-Image bereitstellen"]:
            incomplete = json.loads(jobs().stdout)
            incomplete[0]["jobs"] = [job for job in incomplete[0]["jobs"] if job["name"] != failed]
            gh.side_effect = [response([run()]), subprocess.CompletedProcess([], 0, stdout=json.dumps(incomplete))]
            with self.assertRaisesRegex(RuntimeError, "unvollständig"):
                wait_for_ci("example/repo", SHA)

    @patch("wait_for_commit_ci.subprocess.run")
    def test_success_exports_exact_run_attempt_for_artifact_download(self, gh):
        import tempfile
        from pathlib import Path
        gh.side_effect = [response([run()]), jobs()]
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / "output"
            wait_for_ci("example/repo", SHA, output_path=output)
            self.assertEqual(output.read_text(), "ci_run_id=42\nci_run_attempt=1\n")
        self.assertIn("/attempts/1/jobs", gh.call_args.args[0][-1])

    def test_rejects_non_sha_ref(self):
        with self.assertRaises(ValueError):
            wait_for_ci("example/repo", "main")


if __name__ == "__main__":
    unittest.main()
