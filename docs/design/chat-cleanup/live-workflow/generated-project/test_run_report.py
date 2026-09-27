"""Tests for run_report.sh, the safe finance-export wrapper."""

import os
import shutil
import subprocess
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "run_report.sh")


class SafeExportTests(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.dir)
        self.target = os.path.join(self.dir, "summary.csv")
        self.src = os.path.join(self.dir, "src.csv")

    def write(self, path, text):
        with open(path, "w") as handle:
            handle.write(text)

    def read(self, path):
        with open(path) as handle:
            return handle.read()

    def run_script(self, *args):
        return subprocess.run(
            ["sh", SCRIPT, *args], capture_output=True, text=True, cwd=self.dir
        )

    def stray_temps(self):
        return [n for n in os.listdir(self.dir) if ".tmp." in n]

    def test_failed_strict_run_leaves_existing_target_intact(self):
        self.write(
            self.src,
            "date,category,amount\n"
            "2026-10-05,food,42.50\n"
            "2026-10-12,food,inf\n",
        )
        self.write(self.target, "SENTINEL\n")
        before = self.read(self.target)
        result = self.run_script(
            self.target, self.src, "--month", "2026-10", "--category", "FOOD",
            "--csv", "--strict",
        )
        self.assertEqual(result.returncode, 2)
        self.assertEqual(self.read(self.target), before)
        self.assertIn("skipping invalid amount 'inf'", result.stderr)
        self.assertEqual(self.stray_temps(), [])

    def test_success_replaces_existing_target(self):
        self.write(self.src, "date,category,amount\n2026-10-05,food,42.50\n")
        self.write(self.target, "SENTINEL\n")
        result = self.run_script(
            self.target, self.src, "--month", "2026-10", "--category", "FOOD",
            "--csv", "--strict",
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            self.read(self.target), "category,amount\nfood,42.50\nTOTAL,42.50\n"
        )
        self.assertEqual(self.stray_temps(), [])

    def test_success_creates_missing_target(self):
        self.write(self.src, "date,category,amount\n2026-10-05,food,42.50\n")
        result = self.run_script(self.target, self.src, "--csv", "--strict")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(os.path.exists(self.target))
        self.assertEqual(self.stray_temps(), [])

    def test_missing_arguments_do_not_create_target(self):
        result = self.run_script(self.target)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(os.path.exists(self.target))
        self.assertEqual(self.stray_temps(), [])


if __name__ == "__main__":
    unittest.main()
