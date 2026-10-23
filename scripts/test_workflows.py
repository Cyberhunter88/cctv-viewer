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
        self.pr = (ROOT / ".github/workflows/pr.yml").read_text()
        self.shared = (ROOT / ".github/actions/code-checks/action.yml").read_text()
        self.release = (ROOT / ".github/workflows/release.yml").read_text()

    def job(self, name, pr=False):
        return re.search(rf"^  {name}:\n.*?(?=^  [a-z][a-z-]*:|\Z)", self.pr if pr else self.ci, re.M | re.S)[0]

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
        self.assertIn('  pull_request:\n    branches: [main]', self.pr)
        self.assertNotIn('  pull_request:', self.ci)
        self.assertNotIn('  push:', self.pr)
        self.assertNotIn('packages: write', self.pr)
        self.assertNotIn('  browser:', self.ci)
        self.assertNotIn('  build:', self.pr)
        self.assertNotIn('  publish-commit:', self.pr)
        self.assertNotIn('workflow_call:', self.ci + self.pr)
        for name in ('build', 'publish-commit'):
            job = self.job(name)
            self.assertIn("if: github.event_name == 'push' && github.ref == 'refs/heads/main'", job)
            self.assertIn('packages: write', job)
        self.assertNotIn('contents: write', self.ci)
        self.assertNotRegex(self.ci, r'gh release (?:create|edit)|(?m:^\s+(?:bash )?scripts/promote_image.sh\b)|:latest')

    def test_build_caches_keep_race_and_platform_outputs_separate(self):
        self.assertIn('cache-dependency-path: |\n          go.sum\n          .github/actions/code-checks/action.yml', self.shared)
        build = self.job('build')
        self.assertIn('cache-from: type=gha,scope=commit-${{ matrix.arch }}', build)
        self.assertIn('cache-to: type=gha,mode=max,scope=commit-${{ matrix.arch }}', build)

    def test_shared_checks_use_the_bounded_tool_installer(self):
        self.assertIn('run: bash scripts/install_ci_tools.sh', self.shared)
        self.assertNotIn('sudo apt-get update', self.shared)
        self.assertIn('shellcheck scripts/promote_image.sh scripts/smoke.sh scripts/build_go2rtc.sh scripts/install_ci_tools.sh', self.shared)
        self.assertIn('bash -n scripts/promote_image.sh scripts/smoke.sh scripts/install_ci_tools.sh', self.shared)

    def test_race_tests_run_unconditionally_for_pr_and_main(self):
        code = self.shared
        self.assertRegex(code, r"name: Go-Race-Tests\n      shell: bash\n      run: go test -race \./\.\.\.")
        self.assertNotRegex(code, r"run: go test \./\.\.\.")

    def test_release_job_contract_matches_workflow_names_and_browser_matrix(self):
        from wait_for_commit_ci import REQUIRED_JOBS
        names = {re.search(r"^    name: (.+)$", self.job(job), re.M)[1]
                 for job in ('code-checks', 'build', 'publish-commit', 'test')}
        names.remove('Docker prüfen (${{ matrix.arch }})')
        names.update(f'Docker prüfen ({arch})' for arch in ('amd64', 'arm64'))
        browser = self.job('browser', pr=True)
        template = re.search(r"^    name: (.+)$", browser, re.M)[1]
        entries = re.findall(r"- browser: (\w+)\n            engine: \w+", browser)
        self.assertEqual(entries, ["chrome", "webkit"])
        main_names = names.copy()
        names.update(template.replace('${{ matrix.browser }}', engine) for engine in entries)
        self.assertEqual(REQUIRED_JOBS, main_names)
        self.assertEqual(len(names), 7)
        self.assertEqual(len(names - {"Docker prüfen (amd64)", "Docker prüfen (arm64)",
                                     "Commit-Image bereitstellen"}), 4)
        definitions = re.findall(r"^  ([a-z][a-z-]*):$", self.ci.split("jobs:\n", 1)[1], re.M)
        self.assertEqual(set(definitions), {"code-checks", "build", "publish-commit", "test"})

    def test_required_check_covers_tests_and_main_image_publication(self):
        for pr, required in ((False, ['code-checks', 'build', 'publish-commit']),
                             (True, ['code-checks', 'browser'])):
            gate = self.job('test', pr=pr)
            self.assertIn('name: Anwendung prüfen', gate)
            self.assertIn('if: always()', gate)
            self.assertIn('needs: [' + ', '.join(required) + ']', gate)
        self.assertIn('needs: code-checks', self.job('build'))
        self.assertIn('needs: code-checks', self.job('browser', pr=True))

    def test_code_job_builds_fresh_server_and_both_browsers_run_full_suites(self):
        self.assertNotIn("  test-server:", self.ci)
        server = self.job('code-checks', pr=True)
        browser = self.job('browser', pr=True)
        self.assertIn('npm ci && npm run typecheck && npm test && npm run build', self.shared)
        self.assertLess(server.index('uses: ./.github/actions/code-checks'), server.index('go build -o bin/cctv ./cmd/cctv'))
        self.assertIn('go build -o bin/cctv ./cmd/cctv', server)
        self.assertIn('name: browser-test-server', server)
        self.assertNotIn("if: github.event_name", server)
        self.assertNotIn("Browser-Testserver bauen", self.job("code-checks"))
        upload = server.split('      - uses: actions/upload-artifact@', 1)[1]
        self.assertNotIn("if: github.event_name", upload)
        self.assertIn('needs.code-checks.outputs.commit', browser)
        self.assertIn('bash scripts/install_browser_ci_tools.sh ${{ matrix.browser }}', browser)
        installer = (ROOT / 'scripts/install_browser_ci_tools.sh').read_text()
        self.assertIn('playwright install-deps "$browser"', installer)
        self.assertIn('APT_CONFIG="$config"', installer)
        self.assertIn('https://archive.ubuntu.com/ubuntu/', installer)
        self.assertIn('https://security.ubuntu.com/ubuntu/', installer)
        self.assertIn('timeout 240s', installer)
        self.assertIn('apt-get install -y --no-install-recommends ffmpeg', installer)
        self.assertEqual(browser.count('browser: '), 2)
        self.assertNotIn('shard', browser)
        self.assertIn('run: npm run test:browser', browser)
        self.assertIn('browser-test-results-${{ matrix.browser }}', browser)
        config = (ROOT / 'playwright.config.ts').read_text()
        self.assertIn('globalTimeout:8*60*1000', config)

    def test_required_check_cannot_succeed_after_any_failed_prerequisite(self):
        for pr, required in ((False, ('CODE_RESULT', 'BUILD_RESULT', 'PUBLISH_RESULT')),
                             (True, ('CODE_RESULT', 'BROWSER_RESULT'))):
            shell = self.job('test', pr=pr).split('        run: |\n', 1)[1]
            shell = "\n".join(line[10:] for line in shell.splitlines())
            for values in itertools.product(('success', 'failure', 'skipped', 'cancelled'), repeat=len(required)):
                result = subprocess.run(['bash', '-e', '-c', shell],
                                        env=dict(os.environ, GITHUB_STEP_SUMMARY=os.devnull, GITHUB_SHA='a'*40, **dict(zip(required, values))), capture_output=True)
                self.assertEqual(result.returncode == 0, all(value == 'success' for value in values),
                                 (pr, values, result.stderr))

    def test_pr_and_main_share_code_validation_steps(self):
        main = self.job('code-checks')
        pr = self.job('code-checks', pr=True)
        for check in ('npm ci && npm run typecheck && npm test && npm run build',
                      'go vet ./...', 'go test -race ./...',
                      "python3 -m unittest discover -s scripts -p 'test_*.py'",
                      '"$RUNNER_TEMP/actionlint"', 'shellcheck scripts/promote_image.sh scripts/smoke.sh'):
            self.assertIn(check, self.shared)
        for job in (main, pr):
            self.assertIn('uses: ./.github/actions/code-checks', job)
            self.assertNotIn('go test -race', job)

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

    def test_ci_publishes_only_to_the_separate_build_package(self):
        self.assertIn('echo "image=ghcr.io/${GITHUB_REPOSITORY,,}-builds"', self.shared)
        self.assertIn('echo "image=ghcr.io/${GITHUB_REPOSITORY,,}"', self.release)
        self.assertIn('docker pull "$source_image@$digest"', self.release)
        self.assertIn('skopeo', self.release)
        self.assertLess(self.release.index('name: Geprüftes Multiarch-Image'),
                        self.release.index('name: Öffentlichen Release-Download prüfen'))
        self.assertLess(self.release.index('name: Öffentlichen Release-Download prüfen'),
                        self.release.index('manual_release.py assets'))

    def test_aliases_wait_for_download_and_assets_and_use_canonical_release(self):
        self.assertIn('release: ${{ steps.meta.outputs.release }}', self.release)
        self.assertNotIn('release: ${{ inputs.version }}', self.release)
        self.assertIn("VERSION_ONLY: 'true'", self.release)
        final = self.release.index('name: Versions-Aliases nach Download und Assets freigeben')
        self.assertLess(self.release.index('manual_release.py assets'), final)
        self.assertLess(final, self.release.index('gh release edit'))

    def test_cleanup_is_explicit_manual_and_serialized_with_release(self):
        cleanup = (ROOT / '.github/workflows/cleanup-images.yml').read_text()
        self.assertIn('workflow_dispatch:', cleanup)
        self.assertNotRegex(cleanup, r'(?m)^  (push|pull_request|schedule|workflow_run):')
        self.assertIn("if: github.ref == 'refs/heads/main'", cleanup)
        self.assertIn('default: false', cleanup)
        self.assertIn('group: docker-publication', cleanup)
        self.assertIn('cancel-in-progress: false', cleanup)
        self.assertIn('--delete-version-id', cleanup)
        self.assertIn('ghcr-cleanup-plan', cleanup)
        self.assertIn('actions: read', cleanup)

    def test_scheduled_preview_has_no_write_permissions_or_delete_inputs(self):
        preview = (ROOT / '.github/workflows/cleanup-preview.yml').read_text()
        self.assertIn('schedule:', preview)
        self.assertIn('packages: read', preview)
        self.assertNotIn(': write', preview)
        self.assertNotIn('--delete-version-id', preview)
        self.assertNotIn('inputs:', preview)
        self.assertIn('--build-package', preview)
        self.assertIn('group: ghcr-cleanup-preview', preview)
        self.assertNotIn('group: docker-publication', preview)

    def test_release_reports_all_mutating_step_outcomes_even_on_failure(self):
        from workflow_summary import STAGES
        for identifier, _ in STAGES:
            self.assertIn('- id: ' + identifier, self.release)
        self.assertIn('if: always()', self.release)
        self.assertIn('RELEASE_STEPS: ${{ toJSON(steps) }}', self.release)

    def test_cached_actionlint_is_verified_before_execution(self):
        shared = self.shared
        self.assertIn('key: actionlint-Linux-X64-1.7.12-', shared)
        self.assertLess(shared.index('sha256sum --check'), shared.index('tar -xzf'))
        self.assertLess(shared.index('tar -xzf'), shared.index('"$RUNNER_TEMP/actionlint"'))

    def test_every_third_party_action_is_pinned_to_a_commit(self):
        for workflow in [self.shared, *(path.read_text() for path in (ROOT / ".github/workflows").glob("*.yml"))]:
            for action in re.findall(r'uses: (\S+)', workflow):
                if action.startswith('./'):
                    self.assertTrue((ROOT / action / 'action.yml').is_file())
                else:
                    self.assertRegex(action, r'^[^@]+@[a-f0-9]{40}$')


if __name__ == '__main__':
    unittest.main()
