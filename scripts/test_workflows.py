"""Guard the publication boundaries and required-check contract without extra dependencies."""
from pathlib import Path
import re
import itertools
import os
import subprocess
import unittest

ROOT = Path(__file__).resolve().parent.parent


class WorkflowContractTest(unittest.TestCase):
    def setUp(self):
        self.ci = (ROOT / ".github/workflows/ci.yml").read_text()
        self.release = (ROOT / ".github/workflows/release.yml").read_text()

    def job(self, name):
        return re.search(rf"^  {name}:\n.*?(?=^  [a-z][a-z-]*:|\Z)", self.ci, re.M | re.S)[0]

    def test_release_can_only_be_started_manually_on_main(self):
        triggers = self.release.split("on:\n", 1)[1].split("permissions:\n", 1)[0]
        self.assertRegex(triggers, r"(?m)^  workflow_dispatch:")
        self.assertNotRegex(triggers, r"(?m)^  (release|push|pull_request|workflow_run):")
        self.assertIn('[ "$GITHUB_REF" != refs/heads/main ]', self.release)
        self.assertIn('      version:\n', triggers)
        self.assertIn('required: true', triggers)
        self.assertNotIn('inputs.tag', self.release)

    def test_release_guard_rejects_non_dispatch_and_non_main_invocations(self):
        shell = self.release.split('      - name: Nur manuell auf main ausführen\n', 1)[1]
        shell = shell.split('        run: |\n', 1)[1].split('      - uses:', 1)[0]
        shell = "\n".join(line[10:] for line in shell.splitlines())
        for event, ref in itertools.product(('workflow_dispatch', 'release', 'push'), ('refs/heads/main', 'refs/heads/feature', 'refs/tags/v1.2.3')):
            env = dict(os.environ, GITHUB_EVENT_NAME=event, GITHUB_REF=ref)
            result = subprocess.run(['bash', '-e', '-c', shell], env=env, capture_output=True)
            self.assertEqual(result.returncode == 0, event == 'workflow_dispatch' and ref == 'refs/heads/main')

    def test_pr_and_main_run_ci_but_only_main_push_may_write_images(self):
        self.assertIn('  push:\n    branches: [main]', self.ci)
        self.assertIn('  pull_request:\n    branches: [main]', self.ci)
        for name in ('build', 'publish-commit'):
            job = self.job(name)
            self.assertIn("if: github.event_name == 'push' && github.ref == 'refs/heads/main'", job)
            self.assertIn('packages: write', job)
        self.assertNotIn('contents: write', self.ci)
        self.assertNotRegex(self.ci, r'gh release (?:create|edit)|(?m:^\s+(?:bash )?scripts/promote_image.sh\b)|:latest')

    def test_required_check_covers_tests_and_main_image_publication(self):
        gate = self.job('test')
        self.assertIn('name: Anwendung prüfen', gate)
        self.assertIn('if: always()', gate)
        self.assertIn('needs: [checks, browser, build, publish-commit]', gate)
        for result in ('CODE_RESULT', 'BROWSER_RESULT', 'BUILD_RESULT', 'PUBLISH_RESULT'):
            self.assertIn(f'test "${result}" = success', gate)
        self.assertIn('if [ "$MAIN_PUSH" = true ]', gate)
        self.assertIn('needs: [checks, browser]', self.job('build'))

    def test_required_check_cannot_succeed_after_any_failed_prerequisite(self):
        shell = self.job('test').split('        run: |\n', 1)[1]
        shell = "\n".join(line[10:] for line in shell.splitlines())
        for main in (False, True):
            for values in itertools.product(('success', 'failure', 'skipped', 'cancelled'), repeat=4 if main else 2):
                results = dict(zip(('CODE_RESULT', 'BROWSER_RESULT', 'BUILD_RESULT', 'PUBLISH_RESULT'), values))
                env = dict(os.environ, MAIN_PUSH=str(main).lower(), BUILD_RESULT='skipped', PUBLISH_RESULT='skipped')
                env.update(results)
                result = subprocess.run(['bash', '-e', '-c', shell], env=env, capture_output=True)
                self.assertEqual(result.returncode == 0, all(value == 'success' for value in values), (main, results))

    def test_architecture_smoke_precedes_evidence_upload_and_uses_digest(self):
        job = self.job('build')
        self.assertIn('runner: ubuntu-24.04-arm', job)
        self.assertIn('runner: ubuntu-24.04', job)
        self.assertIn('steps.image.outputs.digest', job)
        self.assertIn('cache-from: type=gha', job)
        self.assertIn("if: steps.source.outputs.exists != 'true'", job)
        self.assertLess(job.index('./scripts/smoke.sh'), job.index('image_registry.py record'))
        self.assertLess(job.index('image_registry.py record'), job.index('actions/upload-artifact@'))
        self.assertIn('commit-image-proof-', self.job('publish-commit'))

    def test_release_never_builds_and_publishes_official_release_last(self):
        self.assertNotIn('build-push-action', self.release)
        self.assertNotIn('docker build ', self.release)
        self.assertLess(self.release.index('CHECK_ONLY=true'), self.release.index('gh release create'))
        self.assertLess(self.release.index('args=('), self.release.index('name: Geprüftes Multiarch-Image'))
        self.assertIn('--generate-notes --draft --latest=false', self.release)
        self.assertLess(self.release.index('manual_release.py assets'), self.release.index('gh release edit'))
        self.assertIn('cancel-in-progress: false', self.release)
        self.assertIn('commit-image-proof-$CI_RUN_ATTEMPT', self.release)
        self.assertIn('DOCKER_CONFIG="$RUNNER_TEMP/anonymous-docker" docker pull "$IMAGE@$digest"', self.release)

    def test_every_third_party_action_is_pinned_to_a_commit(self):
        for workflow in (self.ci, self.release):
            for action in re.findall(r'uses: (\S+)', workflow):
                self.assertRegex(action, r'^[^@]+@[a-f0-9]{40}$')


if __name__ == '__main__':
    unittest.main()
