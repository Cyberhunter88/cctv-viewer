"""Exercise real Git history for ancestry, immutable tags and version validation."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import manual_release


class ManualReleaseTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.previous = Path.cwd()
        self.work = Path(self.tmp.name)
        self.git("init", "-q", "--bare", str(self.work / "origin"), cwd=self.previous)
        self.git("init", "-q", "-b", "main", str(self.work / "work"), cwd=self.previous)
        os.chdir(self.work / "work")
        self.addCleanup(os.chdir, self.previous)
        self.git("config", "user.name", "Release Test")
        self.git("config", "user.email", "test@example.invalid")
        self.git("remote", "add", "origin", str(self.work / "origin"))
        self.version("1.2.3")
        self.git("add", ".")
        self.git("commit", "-qm", "base")
        self.commit = self.git("rev-parse", "HEAD")
        self.git("push", "-q", "origin", "main")
        self.release = None
        original = manual_release.command

        def command(*args, **kwargs):
            if args[:2] == ("gh", "api"):
                return json.dumps(self.release) if self.release is not None else None
            return original(*args, **kwargs)
        self.mock = patch("manual_release.command", side_effect=command).start()
        self.addCleanup(patch.stopall)

    @staticmethod
    def git(*args, cwd=None):
        return subprocess.run(["git", *args], cwd=cwd, check=True, capture_output=True, text=True).stdout.strip()

    def version(self, value, lock=None):
        Path("package.json").write_text(json.dumps({"version": value}))
        Path("package-lock.json").write_text(json.dumps({"version": lock or value, "packages": {"": {"version": lock or value}}}))

    def prepare(self, tag="v1.2.3", commit=""):
        event, output = self.work / "event.json", self.work / "output"
        manual_release.prepare("example/viewer", tag, event, output, commit)
        return json.loads(event.read_text())["release"], output.read_text()

    def test_default_is_remote_main_not_checked_out_branch(self):
        self.git("checkout", "-qb", "feature")
        Path("feature").touch()
        self.git("add", ".")
        self.git("commit", "-qm", "feature")
        release, output = self.prepare()
        self.assertEqual(release, {"tag_name": "v1.2.3", "prerelease": False})
        self.assertIn("commit=" + self.commit, output)
        self.assertIn("create_release=true", output)

    def test_commit_outside_main_is_rejected(self):
        self.git("checkout", "-qb", "feature")
        Path("feature").touch()
        self.git("add", ".")
        self.git("commit", "-qm", "feature")
        with self.assertRaisesRegex(ValueError, "liegt nicht auf main"):
            self.prepare(commit=self.git("rev-parse", "HEAD"))

    def test_requested_ancestor_is_used_when_main_advances(self):
        self.version("1.2.4")
        self.git("add", ".")
        self.git("commit", "-qm", "next")
        self.git("push", "-q", "origin", "main")
        _, output = self.prepare(commit=self.commit)
        self.assertIn("commit=" + self.commit, output)

    def test_version_mismatch_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "stimmt nicht"):
            self.prepare("v9.0.0")

    def test_lock_mismatch_is_rejected(self):
        self.version("1.2.3", "1.2.2")
        self.git("add", ".")
        self.git("commit", "-qm", "bad lock")
        self.git("push", "-q", "origin", "main")
        with self.assertRaisesRegex(ValueError, "package-lock"):
            self.prepare()

    def test_invalid_or_missing_version_and_partial_sha_are_rejected_before_io(self):
        for tag, commit in [("", ""), ("not-semver", ""), ("v1.2.3", "main")]:
            with self.subTest(tag=tag, commit=commit), self.assertRaises(ValueError):
                self.prepare(tag, commit)
        self.mock.assert_not_called()

    def test_existing_annotated_tag_and_release_can_be_repeated(self):
        self.git("tag", "-a", "v1.2.3", "-m", "Release")
        self.git("push", "-q", "origin", "v1.2.3")
        self.release = {"tag_name": "v1.2.3", "prerelease": False, "draft": False}
        _, output = self.prepare()
        self.assertIn("create_release=false", output)

    def test_tag_without_release_can_be_completed(self):
        self.git("tag", "v1.2.3")
        self.git("push", "-q", "origin", "v1.2.3")
        _, output = self.prepare()
        self.assertIn("create_release=true", output)

    def test_existing_tag_cannot_redirect_release(self):
        self.git("tag", "v1.2.3")
        self.git("push", "-q", "origin", "v1.2.3")
        Path("next").touch()
        self.git("add", ".")
        self.git("commit", "-qm", "next")
        self.git("push", "-q", "origin", "main")
        with self.assertRaisesRegex(ValueError, "anderen Commit"):
            self.prepare()
        self.prepare(commit=self.commit)

    def test_matching_partial_draft_is_resumable(self):
        self.release = {"tag_name": "v1.2.3", "prerelease": False, "draft": True, "target_commitish": self.commit}
        _, output = self.prepare()
        self.assertIn("create_release=false", output)

    def test_wrong_draft_or_release_without_tag_is_rejected(self):
        for draft in [False, True]:
            self.release = {"prerelease": False, "draft": draft, "target_commitish": "b" * 40}
            with self.assertRaisesRegex(ValueError, "keinen passenden"):
                self.prepare()

    def test_prerelease_status_comes_from_validated_commit_version(self):
        self.version("1.2.3-beta.1")
        self.git("add", ".")
        self.git("commit", "-qm", "beta")
        self.git("push", "-q", "origin", "main")
        release, _ = self.prepare("v1.2.3-beta.1")
        self.assertTrue(release["prerelease"])
        self.release = {"prerelease": False}
        with self.assertRaisesRegex(ValueError, "Vorabversionsstatus"):
            self.prepare("v1.2.3-beta.1")

    def test_api_permission_error_is_not_absence(self):
        patch.stopall()
        with patch("manual_release.subprocess.run", return_value=subprocess.CompletedProcess([], 1, "", "HTTP 403")):
            with self.assertRaisesRegex(RuntimeError, "403"):
                manual_release.command("gh", "api", "releases", missing_ok=True)


if __name__ == "__main__":
    unittest.main()
