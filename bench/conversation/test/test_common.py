#!/usr/bin/env python3
"""Portable shell-helper tests with both stat dialects under direct control."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
COMMON = ROOT / "lib" / "common.sh"


class FileMtimeTest(unittest.TestCase):
    def invoke(self, system, stat_exit=0, noisy_bsd_probe=False):
        with tempfile.TemporaryDirectory() as tmp:
            tmp = Path(tmp)
            target = tmp / "target"
            target.write_text("evidence")
            calls = tmp / "calls"
            bindir = tmp / "bin"
            bindir.mkdir()
            (bindir / "uname").write_text("#!/bin/sh\nprintf '%s\\n' \"$MOCK_SYSTEM\"\n")
            (bindir / "stat").write_text(
                "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$MOCK_CALLS\"\n"
                "if [ \"$MOCK_NOISY_BSD_PROBE\" = 1 ] && [ \"$1\" = -f ]; then\n"
                "  printf 'GNU filesystem report that is not a timestamp\\n'\n  exit 1\nfi\n"
                "if [ \"$MOCK_STAT_EXIT\" -ne 0 ]; then exit \"$MOCK_STAT_EXIT\"; fi\n"
                "printf '1788931862\\n'\n"
            )
            (bindir / "uname").chmod(0o755)
            (bindir / "stat").chmod(0o755)
            env = os.environ.copy()
            env.update(
                PATH=str(bindir) + os.pathsep + env["PATH"],
                MOCK_SYSTEM=system,
                MOCK_CALLS=str(calls),
                MOCK_STAT_EXIT=str(stat_exit),
                MOCK_NOISY_BSD_PROBE="1" if noisy_bsd_probe else "0",
                COMMON=str(COMMON),
                TARGET=str(target),
            )
            result = subprocess.run(
                ["bash", "-c", 'source "$COMMON"; file_mtime "$TARGET"'],
                text=True,
                capture_output=True,
                env=env,
                check=False,
            )
            return result, calls.read_text().strip()

    def test_gnu_success_uses_only_gnu_stat(self):
        result, call = self.invoke("Linux")
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "1788931862\n")
        self.assertTrue(call.startswith("-c %Y "))

    def test_gnu_never_emits_a_failed_bsd_probe_report(self):
        result, call = self.invoke("Linux", noisy_bsd_probe=True)
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "1788931862\n")
        self.assertNotIn("filesystem report", result.stdout)
        self.assertTrue(call.startswith("-c %Y "))

    def test_bsd_success_uses_only_bsd_stat(self):
        result, call = self.invoke("Darwin")
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "1788931862\n")
        self.assertTrue(call.startswith("-f %m "))

    def test_gnu_failure_is_clean_and_preserved(self):
        result, call = self.invoke("Linux", stat_exit=7)
        self.assertEqual(result.returncode, 7)
        self.assertEqual(result.stdout, "")
        self.assertTrue(call.startswith("-c %Y "))

    def test_bsd_failure_is_clean_and_preserved(self):
        result, call = self.invoke("FreeBSD", stat_exit=9)
        self.assertEqual(result.returncode, 9)
        self.assertEqual(result.stdout, "")
        self.assertTrue(call.startswith("-f %m "))


if __name__ == "__main__":
    unittest.main()
