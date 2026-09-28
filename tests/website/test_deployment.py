import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts/wait-for-website-deployment.sh"
EXPECTED = '{"commit":"new-revision","ref":"refs/heads/main"}\n'
OLD = '{"commit":"old-revision","ref":"refs/heads/main"}\n'


class DeploymentPropagationTests(unittest.TestCase):
    def run_probe(self, responses):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            expected = work / "expected.json"
            expected.write_text(EXPECTED)
            (work / "responses.json").write_text(json.dumps(responses))
            curl = work / "curl"
            curl.write_text("""#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
work = Path(os.environ['PROBE_FIXTURE'])
counter = work / 'attempts'
attempt = int(counter.read_text()) if counter.exists() else 0
counter.write_text(str(attempt + 1))
responses = json.loads((work / 'responses.json').read_text())
response = responses[min(attempt, len(responses) - 1)]
Path(sys.argv[sys.argv.index('-o') + 1]).write_text(response['body'])
sys.exit(response['exit'])
""")
            curl.chmod(0o755)
            sleep = work / "sleep"
            sleep.write_text('#!/bin/sh\nprintf "." >> "$PROBE_FIXTURE/sleeps"\n')
            sleep.chmod(0o755)
            env = dict(os.environ, PATH=f"{work}:{os.environ['PATH']}", PROBE_FIXTURE=str(work))
            result = subprocess.run(
                ["bash", str(SCRIPT), str(expected), "https://example.test/deployment.json?revision=new-revision"],
                env=env, capture_output=True, text=True, timeout=10,
            )
            self.assertEqual(expected.read_text(), EXPECTED)
            attempts = int((work / "attempts").read_text())
            sleeps = len((work / "sleeps").read_text()) if (work / "sleeps").exists() else 0
            return result, attempts, sleeps

    def test_current_revision_succeeds_without_waiting(self):
        result, attempts, sleeps = self.run_probe([{"exit": 0, "body": EXPECTED}])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((attempts, sleeps), (1, 0))

    def test_retries_stale_response_http_failure_and_network_timeout(self):
        result, attempts, sleeps = self.run_probe([
            {"exit": 0, "body": OLD},
            {"exit": 22, "body": EXPECTED},
            {"exit": 28, "body": ""},
            {"exit": 0, "body": EXPECTED},
        ])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((attempts, sleeps), (4, 3))

    def test_never_accepts_wrong_metadata_or_failed_http_response(self):
        for response in [
            {"exit": 0, "body": OLD},
            {"exit": 0, "body": '{"commit":"new-revision","ref":"refs/tags/v0.1.0-rc.5"}\n'},
            {"exit": 22, "body": EXPECTED},
        ]:
            with self.subTest(response=response):
                result, attempts, sleeps = self.run_probe([response])
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual((attempts, sleeps), (12, 11))
                self.assertIn("after 12 attempts", result.stderr)


if __name__ == "__main__":
    unittest.main()
