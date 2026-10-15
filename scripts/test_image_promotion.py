"""Run the real promotion/commit scripts against a registry simulator, without publishing."""
import copy
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from image_registry import ARCHES

ROOT = Path(__file__).resolve().parent.parent
DIGEST = "sha256:" + "1" * 64
OTHER_DIGEST = "sha256:" + "2" * 64
CHILDREN = {"amd64": "sha256:" + "a" * 64, "arm64": "sha256:" + "b" * 64}
IMAGE = "ghcr.io/example/cctv-viewer"
COMMIT = "a" * 40

DOCKER = '''#!/usr/bin/env python3
import hashlib, json, os, sys
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
    print(json.dumps(registry[ref]))
elif args[2] == 'create':
    assert args[3] == '-t', args
    sources = args[5:]
    assert all('@sha256:' in source for source in sources), args
    if len(sources) == 1:
        index = registry[sources[0]]
    else:
        children = {m['digest']:m for source in sources for m in registry[source]['manifests']}
        index = {'manifests': list(children.values())}
        index['digest'] = 'sha256:' + hashlib.sha256(json.dumps(index, sort_keys=True).encode()).hexdigest()
    registry[args[4]] = index
    registry[args[4].split(':', 1)[0]+'@'+index['digest']] = index
    registry['_writes'].append(args[4])
    state.write_text(json.dumps(registry))
else:
    raise AssertionError(args)
'''


def index(digest=DIGEST, children=None):
    return {"digest": digest, "manifests": [dict(digest=value, platform={"os": "linux", "architecture": arch})
                                         for arch, value in (children or CHILDREN).items()]
            + [dict(digest="sha256:" + "f" * 64, platform={"os": "unknown", "architecture": "unknown"},
                    annotations={"vnd.docker.reference.type": "attestation-manifest"})]}


def config(arch, version="1.2.3", commit=COMMIT):
    return {"os": "linux", "architecture": arch, "config": {"Labels": {
        "org.opencontainers.image.version": version, "org.opencontainers.image.revision": commit}}}


class ImagePromotionTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.work = Path(self.tmp.name)
        executable = self.work / "docker"
        executable.write_text(DOCKER)
        executable.chmod(0o755)
        self.registry = {f"{IMAGE}:sha-{COMMIT}": index(), f"{IMAGE}@{DIGEST}": index(), "_writes": []}
        for arch, child in CHILDREN.items():
            self.registry[f"{IMAGE}@{child}"] = config(arch)
        self.proof = dict(schema=1, image=IMAGE, version="1.2.3", commit=COMMIT, digest=DIGEST,
                          platforms=CHILDREN, run_id=42, run_attempt=1)
        self.releases = []
        self.env = dict(os.environ, PATH=self.tmp.name + os.pathsep + os.environ["PATH"],
                        IMAGE=IMAGE, VERSION="1.2.3", COMMIT=COMMIT, RUNNER_TEMP=self.tmp.name,
                        RELEASES_FILE=str(self.work / "releases.json"), PROOF_FILE=str(self.work / "proof.json"),
                        REGISTRY_STATE=str(self.work / "registry.json"), CI_RUN_ID="42", CI_RUN_ATTEMPT="1",
                        GITHUB_RUN_ID="42", GITHUB_RUN_ATTEMPT="1", GITHUB_REPOSITORY="example/cctv-viewer",
                        GITHUB_SERVER_URL="https://github.com", GITHUB_STEP_SUMMARY=str(self.work / "summary"),
                        GITHUB_OUTPUT=str(self.work / "outputs"), RELEASE_PROOF=str(self.work / "release-proof.json"))

    def run_script(self, arguments=None):
        Path(self.env["REGISTRY_STATE"]).write_text(json.dumps(self.registry))
        Path(self.env["PROOF_FILE"]).write_text(json.dumps(self.proof))
        Path(self.env["RELEASES_FILE"]).write_text(json.dumps([self.releases]))
        result = subprocess.run(arguments or ["bash", "scripts/promote_image.sh"], cwd=ROOT,
                                env=self.env, capture_output=True, text=True)
        self.registry = json.loads(Path(self.env["REGISTRY_STATE"]).read_text())
        return result

    def assert_ok(self, result):
        self.assertEqual(result.returncode, 0, result.stderr)

    def add_previous(self, version="2.0.0", aliases=("latest",)):
        children = {arch: "sha256:" + char * 64 for arch, char in zip(ARCHES, ["c", "d"])}
        old = index(OTHER_DIGEST, children)
        self.registry[f"{IMAGE}@{OTHER_DIGEST}"] = old
        for tag in (version, *aliases):
            self.registry[f"{IMAGE}:{tag}"] = old
        for arch, child in children.items():
            self.registry[f"{IMAGE}@{child}"] = config(arch, version)

    def test_release_tags_copy_the_tested_multiarch_index_including_attestations(self):
        self.assert_ok(self.run_script())
        for tag in ("1.2.3", "latest", "1", "1.2"):
            self.assertEqual(self.registry[f"{IMAGE}:{tag}"], index())
        proof = json.loads(Path(self.env["RELEASE_PROOF"]).read_text())
        self.assertEqual(proof["platforms"], CHILDREN)
        self.assertEqual(proof["release"], "v1.2.3")

    def test_repeat_release_reuses_all_correct_artifacts_without_writes(self):
        for tag in ("1.2.3", "latest", "1", "1.2"):
            self.registry[f"{IMAGE}:{tag}"] = index()
        self.assert_ok(self.run_script())
        self.assertEqual(self.registry["_writes"], [])

    def test_conflicting_version_changes_no_alias(self):
        self.add_previous(aliases=("latest", "1.2.3"))
        before = copy.deepcopy(self.registry)
        self.assertNotEqual(self.run_script().returncode, 0)
        self.assertEqual(self.registry, before)

    def test_missing_source_changes_nothing(self):
        self.registry.pop(f"{IMAGE}:sha-{COMMIT}")
        self.assertNotEqual(self.run_script().returncode, 0)
        self.assertEqual(self.registry["_writes"], [])

    def test_registry_errors_are_not_absence(self):
        for error in ("unauthorized", "denied: not found", "429 too many requests", "network unavailable"):
            with self.subTest(error=error):
                self.env["REGISTRY_ERROR"] = error
                self.assertNotEqual(self.run_script().returncode, 0)
                self.assertEqual(self.registry["_writes"], [])

    def test_prerelease_preserves_all_stable_aliases(self):
        self.add_previous(aliases=("latest", "1", "1.2"))
        self.env["VERSION"] = self.proof["version"] = "1.2.3-beta.1"
        for arch, child in CHILDREN.items():
            self.registry[f"{IMAGE}@{child}"] = config(arch, "1.2.3-beta.1")
        self.assert_ok(self.run_script())
        self.assertEqual(self.registry["_writes"], [f"{IMAGE}:1.2.3-beta.1"])
        self.assertEqual(self.registry[f"{IMAGE}:latest"]["digest"], OTHER_DIGEST)

    def test_old_release_cannot_roll_back_latest(self):
        self.add_previous()
        self.releases = [{"tag_name": "v2.0.0"}]
        self.assert_ok(self.run_script())
        self.assertEqual(self.registry[f"{IMAGE}:latest"]["digest"], OTHER_DIGEST)

    def test_newer_unpublished_partial_release_also_cannot_be_rolled_back(self):
        self.add_previous("1.2.4", ("latest", "1", "1.2"))
        self.assert_ok(self.run_script())
        self.assertEqual(self.registry["_writes"], [f"{IMAGE}:1.2.3"])
        self.assertEqual(self.registry[f"{IMAGE}:latest"]["digest"], OTHER_DIGEST)

    def test_unknown_latest_owner_blocks_before_any_tag_changes(self):
        self.registry[f"{IMAGE}:latest"] = index(OTHER_DIGEST)
        result = self.run_script()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Versionszeiger", result.stderr)
        self.assertEqual(self.registry["_writes"], [])

    def test_preflight_is_read_only(self):
        self.env["CHECK_ONLY"] = "true"
        self.assert_ok(self.run_script())
        self.assertEqual(self.registry["_writes"], [])

    def test_build_proof_must_match_exact_ci_attempt_and_commit(self):
        for field, value in [("run_id", 99), ("run_attempt", 2), ("commit", "b" * 40), ("version", "9.0.0")]:
            with self.subTest(field=field):
                original = self.proof[field]
                self.proof[field] = value
                self.assertNotEqual(self.run_script().returncode, 0)
                self.assertEqual(self.registry["_writes"], [])
                self.proof[field] = original

    def test_missing_architecture_or_modified_commit_index_is_rejected(self):
        for change in ("missing", "changed", "duplicate"):
            with self.subTest(change=change):
                altered = index()
                if change == "missing":
                    altered["manifests"].pop(1)
                elif change == "changed":
                    altered["digest"] = OTHER_DIGEST
                else:
                    altered["manifests"].append(altered["manifests"][0])
                self.registry[f"{IMAGE}:sha-{COMMIT}"] = altered
                self.assertNotEqual(self.run_script().returncode, 0)
                self.assertEqual(self.registry["_writes"], [])

    def test_wrong_oci_version_or_revision_is_rejected(self):
        for field, value in [("version", "9.0.0"), ("revision", "b" * 40)]:
            with self.subTest(field=field):
                entry = config("arm64")
                entry["config"]["Labels"][f"org.opencontainers.image.{field}"] = value
                self.registry[f"{IMAGE}@{CHILDREN['arm64']}"] = entry
                self.assertNotEqual(self.run_script().returncode, 0)
                self.assertEqual(self.registry["_writes"], [])

    def test_existing_commit_is_reused_and_smoked_again_by_ci(self):
        self.assert_ok(self.run_script(["python3", "scripts/image_registry.py", "resolve", IMAGE, "1.2.3", COMMIT, "amd64"]))
        outputs = Path(self.env["GITHUB_OUTPUT"]).read_text()
        self.assertIn("exists=true", outputs)
        self.assertIn(f"ref={IMAGE}@{DIGEST}", outputs)
        self.assertEqual(self.registry["_writes"], [])

    def records(self):
        for arch in ARCHES:
            self.assert_ok(self.run_script(["python3", "scripts/image_registry.py", "record", f"{IMAGE}@{DIGEST}", "1.2.3", COMMIT, arch, str(self.work / f"{arch}.json")]))

    def publish(self):
        return self.run_script(["python3", "scripts/image_registry.py", "publish-commit", IMAGE, "1.2.3", COMMIT, self.tmp.name, str(self.work / "ci-proof.json")])

    def test_commit_publication_reuses_existing_index_and_exports_exact_smoke_digests(self):
        self.records()
        self.assert_ok(self.publish())
        proof = json.loads((self.work / "ci-proof.json").read_text())
        self.assertEqual(proof, self.proof)
        self.assertEqual(self.registry["_writes"], [])

    def test_commit_tag_is_created_only_after_both_architecture_records_match(self):
        self.records()
        del self.registry[f"{IMAGE}:sha-{COMMIT}"]
        self.assert_ok(self.publish())
        self.assertEqual(self.registry[f"{IMAGE}:sha-{COMMIT}"], index())

    def test_new_architecture_indexes_are_combined_by_immutable_digest(self):
        for position, arch in enumerate(ARCHES):
            source_digest = "sha256:" + str(position + 3) * 64
            self.registry[f"{IMAGE}@{source_digest}"] = index(source_digest, {arch: CHILDREN[arch]})
            self.assert_ok(self.run_script(["python3", "scripts/image_registry.py", "record", f"{IMAGE}@{source_digest}", "1.2.3", COMMIT, arch, str(self.work / f"{arch}.json")]))
        del self.registry[f"{IMAGE}:sha-{COMMIT}"]
        self.assert_ok(self.publish())
        proof = json.loads((self.work / "ci-proof.json").read_text())
        self.assertEqual(proof["platforms"], CHILDREN)
        self.assertEqual(self.registry[f"{IMAGE}:sha-{COMMIT}"]["digest"], proof["digest"])
        self.assertNotEqual(proof["digest"], DIGEST)

    def test_conflicting_existing_commit_index_is_never_overwritten(self):
        self.records()
        self.add_previous(aliases=(f"sha-{COMMIT}",))
        self.assertNotEqual(self.publish().returncode, 0)
        self.assertEqual(self.registry[f"{IMAGE}:sha-{COMMIT}"]["digest"], OTHER_DIGEST)
        self.assertEqual(self.registry["_writes"], [])

    def test_mismatched_or_missing_smoke_record_blocks_publication(self):
        self.records()
        item = json.loads((self.work / "arm64.json").read_text())
        item["commit"] = "b" * 40
        (self.work / "arm64.json").write_text(json.dumps(item))
        self.assertNotEqual(self.publish().returncode, 0)
        self.assertEqual(self.registry["_writes"], [])
        (self.work / "arm64.json").unlink()
        self.assertNotEqual(self.publish().returncode, 0)
        self.assertEqual(self.registry["_writes"], [])


if __name__ == "__main__":
    unittest.main()
