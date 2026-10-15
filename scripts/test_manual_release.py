import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from manual_release import prepare

SHA = "a" * 40


def result(stdout="", stderr="", code=0):
    return subprocess.CompletedProcess([], code, stdout=stdout, stderr=stderr)


class ManualReleaseTest(unittest.TestCase):
    def prepare(self, tag="v0.6.0", target_commit=""):
        with tempfile.TemporaryDirectory() as tmp:
            event, output = Path(tmp) / "event.json", Path(tmp) / "output"
            prepare("example/viewer", tag, event, output, target_commit)
            return json.loads(event.read_text())["release"], output.read_text()

    @patch("manual_release.subprocess.run")
    def test_new_release_uses_selected_commit_without_publishing(self, run):
        run.side_effect = [result(stderr="HTTP 404", code=1), result(), result(SHA)]
        release, output = self.prepare()
        self.assertEqual(release, {"tag_name": "v0.6.0", "prerelease": False})
        self.assertIn("create_release=true", output)
        self.assertIn(f"commit={SHA}", output)
        self.assertEqual(run.call_args.args[0], ["git", "rev-parse", "HEAD"])
        self.assertEqual(run.call_count, 3)

    @patch("manual_release.subprocess.run")
    def test_existing_release_uses_tag_commit(self, run):
        run.side_effect = [result('{"tag_name":"v0.6.0","draft":false,"prerelease":false}'),
                           result("tag exists"), result(), result(SHA)]
        _, output = self.prepare()
        self.assertIn("create_release=false", output)
        self.assertEqual(run.call_args.args[0], ["git", "rev-parse", "v0.6.0^{commit}"])

    @patch("manual_release.subprocess.run")
    def test_existing_tag_without_release_is_resolved_before_ci(self, run):
        run.side_effect = [result(stderr="HTTP 404", code=1), result("tag exists"), result(), result(SHA)]
        _, output = self.prepare()
        self.assertIn("create_release=true", output)
        self.assertEqual(run.call_args.args[0], ["git", "rev-parse", "v0.6.0^{commit}"])

    @patch("manual_release.subprocess.run")
    def test_prerelease_status_derived_from_validated_tag(self, run):
        run.side_effect = [result(stderr="HTTP 404", code=1), result(), result(SHA)]
        release, _ = self.prepare("v0.6.0-beta.1")
        self.assertTrue(release["prerelease"])

    @patch("manual_release.subprocess.run", return_value=result(stderr="HTTP 403", code=1))
    def test_api_error_does_not_create_synthetic_release(self, run):
        with self.assertRaisesRegex(RuntimeError, "403"):
            self.prepare()
        self.assertEqual(run.call_count, 1)

    @patch("manual_release.subprocess.run", return_value=result('{"draft":true}'))
    def test_draft_is_not_published(self, run):
        with self.assertRaisesRegex(ValueError, "Entwurf"):
            self.prepare()

    @patch("manual_release.subprocess.run")
    def test_invalid_tag_is_rejected_before_api_access(self, run):
        with self.assertRaises(ValueError):
            self.prepare("not-a-version")
        run.assert_not_called()

    @patch("manual_release.subprocess.run")
    def test_requested_commit_is_used_even_if_main_has_advanced(self, run):
        run.side_effect = [result(stderr="HTTP 404", code=1), result()]
        _, output = self.prepare(target_commit=SHA)
        self.assertIn(f"commit={SHA}", output)
        self.assertEqual(run.call_count, 2)

    @patch("manual_release.subprocess.run")
    def test_existing_tag_cannot_redirect_release_to_another_commit(self, run):
        run.side_effect = [result(stderr="HTTP 404", code=1), result("tag exists"),
                           result(), result("b" * 40)]
        with self.assertRaisesRegex(ValueError, "anderen Commit"):
            self.prepare(target_commit=SHA)

    @patch("manual_release.subprocess.run")
    def test_requested_commit_must_be_a_complete_sha(self, run):
        with self.assertRaises(ValueError):
            self.prepare(target_commit="main")
        run.assert_not_called()

    @patch("manual_release.subprocess.run")
    def test_empty_input_uses_package_version(self, run):
        run.side_effect = [result(stderr="HTTP 404", code=1), result(), result(SHA)]
        release, _ = self.prepare("")
        self.assertEqual(release["tag_name"], "v" + json.loads(Path("package.json").read_text())["version"])


if __name__ == "__main__":
    unittest.main()
