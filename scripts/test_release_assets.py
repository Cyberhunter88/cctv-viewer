"""Resume release asset uploads without overwriting published artifacts."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from manual_release import upload_assets


class ReleaseAssetsTest(unittest.TestCase):
    def upload(self, filename="compose.yaml", current=b"same", previous=b"same", exists=True):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / filename
            path.write_bytes(current)
            def command(*args, **kwargs):
                if args[:2] == ("gh", "api"):
                    return json.dumps({"assets": [{"name": filename}] if exists else []})
                if args[:3] == ("gh", "release", "download"):
                    (Path(args[-1]) / filename).write_bytes(previous)
                    return ""
                return ""
            with patch("manual_release.command", side_effect=command) as mock:
                upload_assets("example/viewer", "v1.2.3", str(path))
                return mock.call_args_list

    def test_identical_existing_asset_is_reused(self):
        calls = self.upload()
        self.assertFalse(any(call.args[:3] == ("gh", "release", "upload") for call in calls))

    def test_missing_asset_is_uploaded_without_clobber(self):
        calls = self.upload(exists=False)
        self.assertEqual(calls[-1].args[:3], ("gh", "release", "upload"))
        self.assertNotIn("--clobber", calls[-1].args)

    def test_conflicting_asset_is_not_overwritten(self):
        with self.assertRaisesRegex(ValueError, "widerspricht"):
            self.upload(previous=b"different")

    def test_same_image_proof_from_later_ci_preserves_original_release_record(self):
        proof = dict(schema=1, image="image", version="1.2.3", commit="commit", digest="digest", platforms={"amd64": "a", "arm64": "b"}, release="v1.2.3", run_id=1)
        newer = dict(proof, run_id=2)
        calls = self.upload("release-proof.json", json.dumps(newer).encode(), json.dumps(proof).encode())
        self.assertFalse(any(call.args[:3] == ("gh", "release", "upload") for call in calls))
        with self.assertRaisesRegex(ValueError, "widerspricht"):
            self.upload("release-proof.json", json.dumps(dict(proof, digest="other")).encode(), json.dumps(proof).encode())


if __name__ == '__main__':
    unittest.main()
