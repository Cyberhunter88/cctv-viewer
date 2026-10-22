"""Run the frontend tests from only the inputs copied into the Docker stage."""
import json
from pathlib import Path
import shlex
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent


class DockerFrontendTest(unittest.TestCase):
    def test_frontend_tests_run_with_only_docker_copy_inputs(self):
        node = shutil.which("node")
        self.assertIsNotNone(node, "Node is required to verify the Docker frontend stage")
        with tempfile.TemporaryDirectory(prefix="cctv-docker-frontend-") as directory:
            stage = Path(directory)
            in_frontend = False
            verified = False
            # Replay this stage's local COPY inputs, not a general Dockerfile parser.
            # Stop at the verification RUN so a later COPY cannot hide this failure.
            for line in (ROOT / "Dockerfile").read_text().splitlines():
                instruction = shlex.split(line)
                if not instruction:
                    continue
                if instruction[0] == "FROM":
                    if in_frontend:
                        break
                    in_frontend = instruction[-2:] == ["AS", "frontend"]
                if not in_frontend:
                    continue
                if instruction[0] == "COPY":
                    self.assertFalse(instruction[1].startswith("--"), "Update the stage-input fixture for COPY options")
                    destination = stage / instruction[-1]
                    for source_name in instruction[1:-1]:
                        source = ROOT / source_name
                        if source.is_dir():
                            shutil.copytree(source, destination, dirs_exist_ok=True)
                        else:
                            target = destination / source.name if instruction[-1].endswith("/") or instruction[-1] == "." else destination
                            target.parent.mkdir(parents=True, exist_ok=True)
                            shutil.copyfile(source, target)
                if instruction[0] == "RUN" and "npm test" in line:
                    command = shlex.split(json.loads((stage / "package.json").read_text())["scripts"]["test"])
                    self.assertEqual(command[0], "node", "Update the fixture for a non-Node frontend test command")
                    result = subprocess.run([node, *command[1:]], cwd=stage, capture_output=True, text=True, timeout=60)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    verified = True
                    break
            self.assertTrue(verified, "Docker frontend stage must run npm test")


if __name__ == "__main__":
    unittest.main()
