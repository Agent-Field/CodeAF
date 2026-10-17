#!/usr/bin/env python3
"""Offline tests for scripts/check_homebrew_formula.py."""

import json
import pathlib
import subprocess
import sys
import tempfile
import unittest

from scripts.homebrew_test_support import CHECKSUMS, release_metadata

ROOT = pathlib.Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts" / "check_homebrew_formula.py"
FORMULA = ROOT / "homebrew" / "Formula" / "codeaf.rb"

class HomebrewFormulaTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = pathlib.Path(self.temp.name)
        self.formula = self.directory / "codeaf.rb"
        self.formula.write_text(FORMULA.read_text(encoding="utf-8"), encoding="utf-8")
        self.release = release_metadata()
        self.metadata = self.directory / "release.json"
        self.manifest = self.directory / "checksums.txt"
        self.write_fixtures()

    def write_fixtures(self):
        self.metadata.write_text(json.dumps(self.release), encoding="utf-8")
        self.manifest.write_bytes(CHECKSUMS.encode("utf-8"))

    def run_check(self, *, formula=None):
        return subprocess.run(
            [
                sys.executable,
                str(SCRIPT),
                "--formula",
                str(formula or self.formula),
                "--release-json",
                str(self.metadata),
                "--checksums-file",
                str(self.manifest),
            ],
            capture_output=True,
            text=True,
            check=False,
        )

    def test_valid_formula_matches_release_fixture(self):
        result = self.run_check()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("all 4 platform assets", result.stdout)

    def test_missing_formula_fails(self):
        result = self.run_check(formula=self.directory / "missing.rb")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Formula file does not exist", result.stderr)

    def test_invalid_formula_version_fails(self):
        source = self.formula.read_text(encoding="utf-8").replace(
            'version "0.8.0"', 'version "not-a-version"'
        )
        self.formula.write_text(source, encoding="utf-8")
        result = self.run_check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("stable x.y.z version", result.stderr)

    def test_invalid_formula_class_fails(self):
        source = self.formula.read_text(encoding="utf-8").replace(
            "class Codeaf < Formula", "class Other < Formula"
        )
        self.formula.write_text(source, encoding="utf-8")
        result = self.run_check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must declare class Codeaf < Formula", result.stderr)

    def test_missing_release_asset_fails(self):
        self.release["assets"] = [
            asset
            for asset in self.release["assets"]
            if asset["name"] != "codeaf-linux-arm64"
        ]
        self.write_fixtures()
        result = self.run_check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does not match the GitHub release", result.stderr)

    def test_invalid_checksum_format_fails(self):
        source = self.formula.read_text(encoding="utf-8").replace(
            "f926117df3f9d369bfdaa1960060f1d17bec8df6b3fdc1f25f9b6a9fba18cef2",
            "not-a-checksum",
        )
        self.formula.write_text(source, encoding="utf-8")
        result = self.run_check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("64 hex characters", result.stderr)

    def test_wrong_but_well_formed_checksum_fails(self):
        source = self.formula.read_text(encoding="utf-8").replace(
            "f926117df3f9d369bfdaa1960060f1d17bec8df6b3fdc1f25f9b6a9fba18cef2",
            "0" * 64,
        )
        self.formula.write_text(source, encoding="utf-8")
        result = self.run_check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does not match GitHub's asset digest", result.stderr)

    def test_unsupported_platform_mapping_fails(self):
        source = self.formula.read_text(encoding="utf-8").replace(
            "on_arm do", "on_arm64 do", 1
        )
        self.formula.write_text(source, encoding="utf-8")
        result = self.run_check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("one OS and one architecture selector", result.stderr)

    def test_duplicate_platform_mapping_fails(self):
        source = self.formula.read_text(encoding="utf-8")
        marker = '      sha256 "f926117df3f9d369bfdaa1960060f1d17bec8df6b3fdc1f25f9b6a9fba18cef2"\n'
        self.formula.write_text(source.replace(marker, marker + marker, 1), encoding="utf-8")
        result = self.run_check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("exactly one sha256 for macos/amd64", result.stderr)

    def test_missing_platform_url_fails(self):
        source = self.formula.read_text(encoding="utf-8").replace(
            '      url "https://github.com/Agent-Field/codeaf/releases/download/v#{version}/codeaf-darwin-amd64"\n',
            "",
        )
        self.formula.write_text(source, encoding="utf-8")
        result = self.run_check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("exactly one url for macos/amd64", result.stderr)

    def test_latest_release_version_must_match_formula(self):
        self.release["tag_name"] = "v0.9.0"
        for asset in self.release["assets"]:
            asset["browser_download_url"] = asset["browser_download_url"].replace(
                "/v0.8.0/", "/v0.9.0/"
            )
        self.write_fixtures()
        result = self.run_check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("latest stable release", result.stderr)

    def test_installation_configuration_is_required(self):
        source = self.formula.read_text(encoding="utf-8").replace(
            'bin.install executables.first => "codeaf"',
            'bin.install executables.first => "other"',
        )
        self.formula.write_text(source, encoding="utf-8")
        result = self.run_check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("install the downloaded executable as bin/codeaf", result.stderr)

    def test_manifest_mismatch_fails(self):
        self.manifest.write_bytes(CHECKSUMS.replace("f926", "0f26", 1).encode("utf-8"))
        result = self.run_check()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does not match GitHub's asset digest", result.stderr)


if __name__ == "__main__":
    unittest.main()
