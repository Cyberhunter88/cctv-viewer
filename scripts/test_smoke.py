"""Run the real smoke script against a synthetic Docker/HTTP boundary."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
DOCKER = '''#!/usr/bin/env python3
import os,sys
args=sys.argv[1:]
if args[:2]==['image','inspect']:
    template=args[3]
    if 'Architecture' in template: print(os.environ['SMOKE_ARCH'])
    elif '.Os' in template: print('linux')
    elif 'image.version' in template: print(os.environ['SMOKE_LABEL'])
    elif 'image.revision' in template: print('a'*40)
    else: print('1024')
elif args[0]=='run' and args[-1]=='version': print(os.environ['SMOKE_VERSION'])
elif args[0]=='exec' and args[-1]=='-version': print(os.environ.get('SMOKE_LIVE_VERSION','go2rtc version 1.9.14'))
elif args[0]=='port': print('127.0.0.1:12345')
elif args[0]=='inspect': print('10001:10001' if 'Config.User' in args[2] else 'false')
'''
CURL = '''#!/usr/bin/env python3
import sys
url=sys.argv[-1]
if url.endswith('/api/cameras'): print('401',end='')
elif url.endswith('/api/live'): print('{\"ready\":true}')
elif url.endswith('/api/session'): print('{"role":"admin"}')
elif url.endswith('/'): print('<html lang="de">')
'''


class SmokeTest(unittest.TestCase):
    def run_smoke(self, **changes):
        with tempfile.TemporaryDirectory() as tmp:
            for name, source in [('docker', DOCKER), ('curl', CURL)]:
                path = Path(tmp) / name
                path.write_text(source)
                path.chmod(0o755)
            env = dict(os.environ, PATH=tmp+os.pathsep+os.environ['PATH'],
                       SMOKE_ARCH='amd64', SMOKE_LABEL='1.2.3', SMOKE_VERSION='1.2.3')
            env.update(changes)
            return subprocess.run(['bash', 'scripts/smoke.sh', 'image@sha256:example', '1.2.3', 'a'*40, 'amd64'],
                                  cwd=ROOT, env=env, capture_output=True, text=True)

    def test_valid_native_image_passes_existing_runtime_checks(self):
        result = self.run_smoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('Docker-Smoke-Test erfolgreich', result.stdout)

    def test_missing_live_binary_blocks_image(self):
        self.assertNotEqual(self.run_smoke(SMOKE_LIVE_VERSION="unavailable").returncode, 0)

    def test_wrong_binary_version_blocks_release(self):
        result = self.run_smoke(SMOKE_VERSION='dev')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Anwendungsversion', result.stdout)

    def test_wrong_architecture_or_label_blocks_release(self):
        for mismatch in [dict(SMOKE_ARCH='arm64'), dict(SMOKE_LABEL='9.0.0')]:
            with self.subTest(mismatch=mismatch):
                self.assertNotEqual(self.run_smoke(**mismatch).returncode, 0)


if __name__ == '__main__':
    unittest.main()
