"""Exercise the publication script with a filesystem-backed Wrangler boundary."""

import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
VERSION = "v0.1.0"


class PublicationTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.assets = self.root / "assets"
        self.remote = self.root / "remote"
        self.remote.mkdir()
        self.version = VERSION
        subprocess.run(
            ["sh", str(ROOT / "scripts/package-installers.sh"), VERSION, str(self.assets)],
            check=True,
        )
        # Deliberately include data that must never leave GitHub.
        (self.assets / "arveld-linux.tar.gz").write_bytes(b"private binary")
        self.write_checksums()
        tools = self.root / "tools"
        tools.mkdir()
        wrangler = tools / "wrangler"
        wrangler.write_text("""#!/usr/bin/env python3
import json, os, shutil, sys
from pathlib import Path
args = sys.argv[1:]
assert args[:3] == ['r2', 'object', 'put'] and '--remote' in args
remote = Path(os.environ['TEST_REMOTE'])
with (remote / 'calls.jsonl').open('a') as log:
    log.write(json.dumps(args) + '\\n')
if args[3] == os.environ.get('TEST_FAIL_KEY'):
    sys.exit(1)
target = remote / args[3]
target.parent.mkdir(parents=True, exist_ok=True)
shutil.copyfile(args[args.index('--file') + 1], target)
""")
        wrangler.chmod(0o755)
        self.env = {
            **os.environ,
            "PATH": f"{tools}:{os.environ['PATH']}",
            "ARVELD_INSTALLERS_BUCKET": "arveld-install",
            "TEST_REMOTE": str(self.remote),
            "PROMOTE_INSTALLERS": "false",
        }

    def write_checksums(self):
        lines = [
            f"{hashlib.sha256(path.read_bytes()).hexdigest()}  ./{path.name}\n"
            for path in sorted(self.assets.iterdir()) if path.name != "SHA256SUMS"
        ]
        (self.assets / "SHA256SUMS").write_text("".join(lines))

    def publish(self, **env):
        return subprocess.run(
            ["sh", str(ROOT / "scripts/publish-installers.sh"), self.version, str(self.assets)],
            env={**self.env, **env}, capture_output=True, text=True,
        )

    def calls(self):
        path = self.remote / "calls.jsonl"
        return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []

    def test_only_verified_scripts_and_selected_checksums_are_public(self):
        """Publish only verified scripts/checksums and promote an eligible stable release."""
        result = self.publish(PROMOTE_INSTALLERS="true")
        self.assertEqual(result.returncode, 0, result.stderr)
        bucket = self.remote / "arveld-install"
        expected = {
            f"{VERSION}/install-arveld.sh", f"{VERSION}/install-arveld-agent.sh",
            f"{VERSION}/SHA256SUMS", "install-arveld.sh", "install-arveld-agent.sh",
        }
        self.assertEqual({str(p.relative_to(bucket)) for p in bucket.rglob("*") if p.is_file()}, expected)
        for component in ("arveld", "arveld-agent"):
            name = f"install-{component}.sh"
            self.assertEqual((bucket / name).read_bytes(), (self.assets / name).read_bytes())
            self.assertEqual((bucket / VERSION / name).read_bytes(), (self.assets / name).read_bytes())
        checksums = (bucket / VERSION / "SHA256SUMS").read_text().splitlines()
        self.assertEqual(len(checksums), 2)
        for line in checksums:
            digest, name = line.split()
            self.assertEqual(digest, hashlib.sha256((bucket / VERSION / name).read_bytes()).hexdigest())
        for index, call in enumerate(self.calls()):
            cache = call[call.index("--cache-control") + 1]
            self.assertEqual(cache, "public, max-age=31536000, immutable" if index < 3 else "no-store")

    def test_recovery_does_not_change_the_promoted_release(self):
        """Publish versioned files during recovery while preserving the existing root URL."""
        bucket = self.remote / "arveld-install"
        bucket.mkdir()
        alias = bucket / "install-arveld.sh"
        alias.write_text("previous promotion")
        result = self.publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(alias.read_text(), "previous promotion")
        self.assertEqual(len(self.calls()), 3)

    def test_prereleases_never_replace_root_installers(self):
        """Reject prerelease promotion before upload; allow versioned publication without changing root URLs."""
        bucket = self.remote / "arveld-install"
        bucket.mkdir()
        for name in ("install-arveld.sh", "install-arveld-agent.sh"):
            (bucket / name).write_text("previous promotion")
        for suffix in ("rc.4", "beta.1", "alpha", "1"):
            with self.subTest(suffix=suffix):
                self.version = f"v0.2.0-{suffix}"
                subprocess.run(
                    ["sh", str(ROOT / "scripts/package-installers.sh"), self.version, str(self.assets)],
                    check=True,
                )
                self.write_checksums()
                before = self.calls()
                rejected = self.publish(PROMOTE_INSTALLERS="true")
                self.assertNotEqual(rejected.returncode, 0)
                self.assertIn("stable release", rejected.stderr)
                self.assertEqual(self.calls(), before)
                versioned = self.publish()
                self.assertEqual(versioned.returncode, 0, versioned.stderr)
                self.assertEqual(len(self.calls()), len(before) + 3)
                for name in ("install-arveld.sh", "install-arveld-agent.sh"):
                    self.assertEqual((bucket / name).read_text(), "previous promotion")
                    self.assertEqual((bucket / self.version / name).read_bytes(), (self.assets / name).read_bytes())

    def test_corruption_missing_and_duplicate_checksums_fail_before_upload(self):
        """Expected rejections: corrupt scripts and missing/duplicate checksums must cause zero uploads."""
        original = (self.assets / "SHA256SUMS").read_text()
        script = self.assets / "install-arveld-agent.sh"
        body = script.read_text()
        for scenario in ("corrupt", "missing", "duplicate"):
            with self.subTest(scenario=scenario):
                script.write_text(body + "\n# corrupted\n" if scenario == "corrupt" else body)
                checksum = original
                if scenario == "missing":
                    checksum = "".join(line for line in original.splitlines(True) if "install-arveld-agent.sh" not in line)
                elif scenario == "duplicate":
                    checksum += original
                (self.assets / "SHA256SUMS").write_text(checksum)
                self.assertNotEqual(self.publish(PROMOTE_INSTALLERS="true").returncode, 0)
                self.assertEqual(self.calls(), [])

    def test_wrong_release_or_component_is_rejected_even_with_valid_checksums(self):
        """Expected rejections: valid checksums cannot authorize the wrong version or component."""
        script = self.assets / "install-arveld-agent.sh"
        original = script.read_text()
        for body in (
            original.replace(VERSION, "v9.0.0"),
            original.replace("export ARVELD_INSTALL_COMPONENT=arveld-agent", "export ARVELD_INSTALL_COMPONENT=arveld"),
        ):
            with self.subTest(body=body[:100]):
                script.write_text(body)
                self.write_checksums()
                self.assertNotEqual(self.publish().returncode, 0)
                self.assertEqual(self.calls(), [])

    def test_failed_versioned_upload_never_updates_aliases(self):
        """An intentionally failed versioned upload must prevent every root URL update."""
        result = self.publish(
            PROMOTE_INSTALLERS="true",
            TEST_FAIL_KEY=f"arveld-install/{VERSION}/install-arveld-agent.sh",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(len(self.calls()), 2)
        self.assertTrue(all(f"/{VERSION}/" in call[3] for call in self.calls()))

    def test_bootstrap_requires_a_version_and_cannot_be_published_as_a_release(self):
        """Expected rejections: unversioned bootstrap scripts cannot masquerade as release assets."""
        subprocess.run(
            ["sh", str(ROOT / "scripts/package-installers.sh"), "--unversioned", str(self.assets)],
            check=True,
        )
        self.write_checksums()
        self.assertNotEqual(self.publish().returncode, 0)
        self.assertEqual(self.calls(), [])
        for component in ("arveld", "arveld-agent"):
            result = subprocess.run(
                ["sh", str(self.assets / f"install-{component}.sh")],
                env={**os.environ, "ARVELD_VERSION": ""}, capture_output=True, text=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("Set ARVELD_VERSION to an exact release tag", result.stderr)


if __name__ == "__main__":
    unittest.main()
