"""Offline grader regressions; run on Spark before scoring the new cohort."""
import subprocess
import tempfile
import unittest
from pathlib import Path

from grading import restore_tests, test_outcomes


class GraderTests(unittest.TestCase):
    def test_candidate_cannot_replace_nested_regressions_with_easy_tests(self):
        with tempfile.TemporaryDirectory() as temp:
            repo = Path(temp)
            tests = repo / "package/tests"
            tests.mkdir(parents=True)
            original = tests / "test_existing.py"
            original.write_text("assert difficult_requirement()\n")
            product = repo / "package/implementation.py"
            product.write_text("old implementation\n")

            def git(*args):
                return subprocess.check_output(["git", *args], cwd=repo, text=True).strip()

            git("init", "-q")
            git("config", "user.name", "Fixture")
            git("config", "user.email", "fixture@localhost")
            git("add", "package/tests/test_existing.py", "package/implementation.py")
            git("commit", "-qm", "Frozen fixture")
            base = git("rev-parse", "HEAD")
            original.unlink()
            replacement = tests / "test_easy.py"
            replacement.write_text("assert True\n")
            product.write_text("candidate implementation\n")

            restore_tests(repo, base, ["package/tests"])

            self.assertEqual(original.read_text(), "assert difficult_requirement()\n")
            self.assertFalse(replacement.exists())
            self.assertEqual(product.read_text(), "candidate implementation\n")

    def test_green_exit_cannot_hide_missing_duplicate_or_skipped_cases(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "tests.xml"
            path.write_text('<testsuites><testsuite><testcase classname="A" name="x"/>'
                            '<testcase classname="A" name="x"/></testsuite></testsuites>')
            reference = test_outcomes(path)
            path.write_text('<testsuites><testsuite><testcase classname="A" name="x"/>'
                            '</testsuite></testsuites>')
            self.assertNotEqual(reference, test_outcomes(path))
            path.write_text('<testsuites><testsuite><testcase classname="A" name="x"/>'
                            '<testcase classname="A" name="x"><skipped/></testcase>'
                            '</testsuite></testsuites>')
            self.assertNotEqual(reference, test_outcomes(path))

    def test_fixture_paths_cannot_escape_the_grading_checkout(self):
        with tempfile.TemporaryDirectory() as temp:
            for root in ("../tests", "/tests", "."):
                with self.subTest(root=root), self.assertRaises(ValueError):
                    restore_tests(Path(temp), "unused", [root])


if __name__ == "__main__":
    unittest.main()
