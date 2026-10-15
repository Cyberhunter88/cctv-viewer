"""Exercise the real promotion shell script against a simulated registry CLI."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
DIGEST = "sha256:" + "1" * 64
OTHER_DIGEST = "sha256:" + "2" * 64
IMAGE = "ghcr.io/example/cctv-viewer"
COMMIT = "a" * 40

# The shell script must only inspect or copy manifests, never build an image.
DOCKER = '''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
state = Path(os.environ['REGISTRY_STATE'])
registry = json.loads(state.read_text())
args = sys.argv[1:]
assert args[:2] == ['buildx', 'imagetools'], args
if args[2] == 'inspect':
    ref = args[3]
    if ref not in registry:
        print(os.environ.get('REGISTRY_ERROR', 'manifest unknown'), file=sys.stderr)
        sys.exit(1)
    print(json.dumps({'digest': registry[ref]}))
elif args[2] == 'create':
    assert args[3] == '-t', args
    assert '@sha256:' in args[5], args
    registry[args[4]] = args[5].split('@', 1)[1]
    state.write_text(json.dumps(registry))
else:
    raise AssertionError(args)
'''


class ImagePromotionTest(unittest.TestCase):
    def promote(self, version="1.2.3", existing=None, releases=None, error=None, source=True):
        with tempfile.TemporaryDirectory() as tmp:
            work = Path(tmp)
            docker = work / "docker"
            docker.write_text(DOCKER)
            docker.chmod(0o755)
            registry = {f"{IMAGE}:sha-{COMMIT}": DIGEST} if source else {}
            registry.update(existing or {})
            state = work / "registry.json"
            state.write_text(json.dumps(registry))
            release_file = work / "releases.json"
            release_file.write_text(json.dumps([releases or []]))
            env = dict(os.environ, PATH=tmp + os.pathsep + os.environ["PATH"],
                       IMAGE=IMAGE, VERSION=version, COMMIT=COMMIT,
                       RUNNER_TEMP=tmp, RELEASES_FILE=str(release_file),
                       REGISTRY_STATE=str(state), GITHUB_STEP_SUMMARY=str(work / "summary"))
            if error:
                env["REGISTRY_ERROR"] = error
            result = subprocess.run(["bash", "scripts/promote_image.sh"], cwd=ROOT,
                                    env=env, capture_output=True, text=True)
            return result, json.loads(state.read_text())

    def test_release_tags_match_tested_digest(self):
        result, registry = self.promote()
        self.assertEqual(result.returncode, 0, result.stderr)
        for tag in ("1.2.3", "latest", "1", "1.2"):
            self.assertEqual(registry[f"{IMAGE}:{tag}"], DIGEST)

    def test_repeat_release_accepts_identical_version(self):
        result, registry = self.promote(existing={f"{IMAGE}:1.2.3": DIGEST})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(registry[f"{IMAGE}:latest"], DIGEST)

    def test_conflicting_version_changes_no_alias(self):
        existing = {f"{IMAGE}:1.2.3": OTHER_DIGEST, f"{IMAGE}:latest": OTHER_DIGEST}
        result, registry = self.promote(existing=existing)
        self.assertNotEqual(result.returncode, 0)
        for tag, digest in existing.items():
            self.assertEqual(registry[tag], digest)
        self.assertNotIn(f"{IMAGE}:1", registry)

    def test_missing_source_changes_nothing(self):
        result, registry = self.promote(source=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(registry, {})

    def test_registry_error_is_not_treated_as_missing_version(self):
        result, registry = self.promote(error="unauthorized")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(list(registry), [f"{IMAGE}:sha-{COMMIT}"])

    def test_prerelease_preserves_latest(self):
        result, registry = self.promote(version="2.0.0-beta.1",
                                       existing={f"{IMAGE}:latest": OTHER_DIGEST})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(registry[f"{IMAGE}:2.0.0-beta.1"], DIGEST)
        self.assertEqual(registry[f"{IMAGE}:latest"], OTHER_DIGEST)

    def test_old_release_cannot_roll_back_latest(self):
        result, registry = self.promote(existing={f"{IMAGE}:latest": OTHER_DIGEST},
                                       releases=[{"tag_name": "v2.0.0"}])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(registry[f"{IMAGE}:1.2.3"], DIGEST)
        self.assertEqual(registry[f"{IMAGE}:latest"], OTHER_DIGEST)


if __name__ == "__main__":
    unittest.main()
