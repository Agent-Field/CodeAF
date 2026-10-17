"""Offline tests for scripts/check_homebrew_cask.py."""

import json
import pathlib
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))

from homebrew_test_support import CHECKSUMS, release_metadata

SCRIPT = ROOT / "scripts" / "check_homebrew_cask.py"
CASK = ROOT / "homebrew" / "Casks" / "codeaf.rb"


class HomebrewCaskTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = pathlib.Path(self.temp.name)
        self.cask = self.directory / "codeaf.rb"
        shutil.copyfile(CASK, self.cask)
        self.release = release_metadata()
        self.metadata = self.directory / "release.json"
        self.manifest = self.directory / "checksums.txt"
        self.write_fixtures()

    def write_fixtures(self):
        self.metadata.write_text(json.dumps(self.release), encoding="utf-8")
        self.manifest.write_bytes(CHECKSUMS.encode("utf-8"))

    def run_check(self, *, cask=None):
        return subprocess.run(
            [
                sys.executable,
                str(SCRIPT),
                "--cask",
                str(cask or self.cask),
                "--release-json",
                str(self.metadata),
                "--checksums-file",
                str(self.manifest),
            ],
            capture_output=True,
            text=True,
            check=False,
        )

    def test_valid_cask_matches_release_fixture(self):
        result = self.run_check()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("both macOS architecture assets", result.stdout)

    def test_missing_cask_fails(self):
        result = self.run_check(cask=self.directory / "missing.rb")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Cask file does not exist", result.stderr)

    def test_stale_version_fails(self):
        source = self.cask.read_text(encoding="utf-8").replace(
            'version "0.8.0"', 'version "0.7.0"'
        )
        self.cask.write_text(source, encoding="utf-8")
        result = self.run_check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("latest stable release", result.stderr)

    def test_arm_checksum_must_match_release(self):
        source = self.cask.read_text(encoding="utf-8").replace(
            "19b55289b5d8bf3345b5e3fae0682cc75ed0a05f5207f772f9f60c6d303589bf",
            "0" * 64,
        )
        self.cask.write_text(source, encoding="utf-8")
        result = self.run_check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does not match the official release checksums", result.stderr)

    def test_untrusted_url_fails(self):
        source = self.cask.read_text(encoding="utf-8").replace(
            "https://github.com/Agent-Field/codeaf/releases/",
            "https://example.com/codeaf/releases/",
        )
        self.cask.write_text(source, encoding="utf-8")
        result = self.run_check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Cask URL must select the official", result.stderr)

    def test_cask_must_set_executable_permissions(self):
        source = self.cask.read_text(encoding="utf-8").replace(
            '    set_permissions 0755, staged_path/"codeaf-darwin-*"\n', ""
        )
        self.cask.write_text(source, encoding="utf-8")
        result = self.run_check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("make the downloaded raw executable runnable", result.stderr)

    def test_missing_architecture_asset_fails_closed(self):
        self.release["assets"] = [
            asset
            for asset in self.release["assets"]
            if asset["name"] != "codeaf-darwin-arm64"
        ]
        self.write_fixtures()
        result = self.run_check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does not match the GitHub release", result.stderr)

    def test_manifest_mismatch_fails_closed(self):
        self.manifest.write_bytes(CHECKSUMS.replace("f926", "0f26", 1).encode("utf-8"))
        result = self.run_check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does not match GitHub's asset digest", result.stderr)


if __name__ == "__main__":
    unittest.main()
