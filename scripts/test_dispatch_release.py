import json
import subprocess
import unittest
from unittest.mock import patch

from dispatch_release import dispatch

SHA = "a" * 40
REPO = "example/viewer"


def result(value="", code=0):
    return subprocess.CompletedProcess([], code, stdout=value if not code else "",
                                       stderr=value if code else "")


class DispatchReleaseTest(unittest.TestCase):
    @patch("dispatch_release.subprocess.run")
    def test_new_version_dispatches_exact_tested_commit(self, run):
        run.side_effect = [result("HTTP 404", 1), result()]
        self.assertTrue(dispatch(REPO, SHA, "0.6.0"))
        self.assertEqual(run.call_args.args[0], [
            "gh", "workflow", "run", "release.yml", "--repo", REPO,
            "--ref", "main", "-f", "tag=v0.6.0", "-f", f"commit={SHA}"])

    @patch("dispatch_release.subprocess.run")
    def test_existing_version_on_other_commit_is_not_overwritten(self, run):
        run.side_effect = [result('{"draft":false}'), result(json.dumps({"sha": "b" * 40}))]
        self.assertFalse(dispatch(REPO, SHA, "0.6.0"))
        self.assertEqual(run.call_count, 2)

    @patch("dispatch_release.subprocess.run")
    def test_same_commit_can_retry_publication(self, run):
        run.side_effect = [result('{"draft":false}'), result(json.dumps({"sha": SHA})), result()]
        self.assertTrue(dispatch(REPO, SHA, "0.6.0"))
        self.assertEqual(run.call_count, 3)

    @patch("dispatch_release.subprocess.run", return_value=result("HTTP 403", 1))
    def test_api_failure_never_dispatches(self, run):
        with self.assertRaisesRegex(RuntimeError, "403"):
            dispatch(REPO, SHA, "0.6.0")
        self.assertEqual(run.call_count, 1)

    @patch("dispatch_release.subprocess.run", return_value=result('{"draft":true}'))
    def test_draft_needs_manual_resolution(self, run):
        with self.assertRaisesRegex(ValueError, "Entwurf"):
            dispatch(REPO, SHA, "0.6.0")
        self.assertEqual(run.call_count, 1)

    @patch("dispatch_release.subprocess.run")
    def test_invalid_version_or_commit_never_dispatches(self, run):
        for commit, version in [("main", "0.6.0"), (SHA, "foo"), (SHA, "01.0.0")]:
            with self.assertRaises(ValueError):
                dispatch(REPO, commit, version)
        run.assert_not_called()

    @patch("dispatch_release.subprocess.run")
    def test_dispatch_failure_is_reported(self, run):
        run.side_effect = [result("HTTP 404", 1), result("dispatch failed", 1)]
        with self.assertRaisesRegex(RuntimeError, "dispatch failed"):
            dispatch(REPO, SHA, "0.6.0")


if __name__ == "__main__":
    unittest.main()
