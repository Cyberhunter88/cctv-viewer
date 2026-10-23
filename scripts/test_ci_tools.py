"""Exercise the actual CI installer at the package-manager boundary, offline."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
SOURCES = """Types: deb
URIs: mirror+file:/etc/apt/apt-mirrors.txt
Suites: noble noble-updates noble-backports
Components: main universe
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg

Types: deb
URIs: mirror+file:/etc/apt/apt-mirrors-security.txt
Suites: noble-security
Components: main universe
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg
"""
SUDO = '''import json,os,pathlib,sys
args=sys.argv[1:]
source=next(arg.split('=',1)[1] for arg in args if arg.startswith('Dir::Etc::sourcelist='))
with open(os.environ['MOCK_LOG'],'a') as log:
    log.write(json.dumps({'args':args,'sources':pathlib.Path(source).read_text()})+'\\n')
if os.environ.get('MOCK_FAIL') in args: sys.exit(17)
if 'install' in args:
    for package in args[args.index('--no-install-recommends')+1:]:
        for tool in (['ffmpeg','ffprobe'] if package=='ffmpeg' else [package]):
            file=pathlib.Path(os.environ['MOCK_BIN'])/tool
            file.write_text('#!/bin/sh\\nexit 0\\n');file.chmod(0o755)
'''


class CIToolsTest(unittest.TestCase):
    def run_installer(self, available=(), fail='', sources=SOURCES):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binaries = root/'bin'
            binaries.mkdir()
            for name in ['sed', 'mktemp', 'rm']:
                (binaries/name).symlink_to(shutil.which(name))
            for name in available:
                executable = binaries/name
                executable.write_text('#!/bin/sh\nexit 0\n')
                executable.chmod(0o755)
            sudo = binaries/'sudo'
            sudo.write_text('#!'+sys.executable+'\n'+SUDO)
            sudo.chmod(0o755)
            source = root/'runner.sources'
            source.write_text(sources)
            log = root/'calls.jsonl'
            result = subprocess.run([shutil.which('bash'), str(ROOT/'scripts/install_ci_tools.sh'), str(source)],
                                    env=dict(os.environ, PATH=str(binaries), RUNNER_TEMP=str(root),
                                             MOCK_BIN=str(binaries), MOCK_LOG=str(log), MOCK_FAIL=fail),
                                    text=True, capture_output=True, timeout=10)
            calls = [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []
            self.assertEqual(source.read_text(), sources, 'system source file was modified')
            self.assertEqual(list(root.glob('cctv-apt.*')), [], 'temporary sources were not cleaned')
            return result, calls

    def test_available_tools_do_not_touch_apt(self):
        result, calls = self.run_installer(['ffmpeg', 'ffprobe', 'shellcheck'])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(calls, [])

    def test_direct_mirrors_preserve_suites_signatures_and_install_only_missing_packages(self):
        result, calls = self.run_installer(['shellcheck'])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(calls), 2)
        self.assertEqual(calls[0]['args'][:3], ['timeout', '120s', 'apt-get'])
        self.assertEqual(calls[0]['args'][-1], 'update')
        self.assertEqual(calls[1]['args'][:3], ['timeout', '180s', 'apt-get'])
        self.assertEqual(calls[1]['args'][-4:], ['install', '-y', '--no-install-recommends', 'ffmpeg'])
        for call in calls:
            self.assertIn('Dir::Etc::sourceparts=-', call['args'])
            self.assertIn('https://archive.ubuntu.com/ubuntu/', call['sources'])
            self.assertIn('https://security.ubuntu.com/ubuntu/', call['sources'])
            self.assertIn('Suites: noble-security', call['sources'])
            self.assertEqual(call['sources'].count('Signed-By:'), 2)
            self.assertNotIn('mirror+file:', call['sources'])

    def test_missing_ffprobe_installs_ffmpeg_package_once(self):
        result, calls = self.run_installer(['ffmpeg', 'shellcheck'])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(calls[-1]['args'].count('ffmpeg'), 1)

    def test_azure_source_is_replaced_without_disabling_authentication(self):
        result, calls = self.run_installer(['ffmpeg', 'ffprobe'], sources=SOURCES.replace(
            'mirror+file:/etc/apt/apt-mirrors.txt', 'http://azure.archive.ubuntu.com/ubuntu/'))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn('azure.archive', calls[0]['sources'])
        self.assertEqual(calls[-1]['args'][-1], 'shellcheck')
        self.assertNotIn('--allow-unauthenticated', calls[-1]['args'])

    def test_update_and_install_errors_are_not_ignored(self):
        for operation, count in [('update', 1), ('install', 2)]:
            with self.subTest(operation=operation):
                result, calls = self.run_installer(fail=operation)
                self.assertEqual(result.returncode, 17)
                self.assertEqual(len(calls), count)


if __name__ == '__main__':
    unittest.main()
