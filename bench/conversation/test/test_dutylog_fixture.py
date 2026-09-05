"""Counterexamples for the multi-defect fixture and its external judge.

No model, no network, no spend: this builds the `multi-defect-pipeline` fixture
the way run.sh does, repairs it in the ways a harness might, and checks that the
judge says the right thing about each of them.

The four properties worth having, and the ones this file exists to keep:

  red before      the fixture as handed over fails, and fails in each of the
                  four modules separately — a defect nothing detects is not a
                  defect, it is decoration
  green after     the reference repair, kept outside the workspace, passes every
                  case; so does a completely different correct implementation,
                  because the judge marks behaviour and not text
  partial fails   three modules out of four is not a pass, and the integrated
                  pipeline is what says so
  forgery fails   a hard-coded answer to the visible tests, an edited test file
                  and a planted verdict are each caught

Run it with:

    python3 -m unittest discover -s bench/conversation/test -p 'test_*.py'
"""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

CONV_ROOT = Path(__file__).resolve().parents[1]
FIXTURE = CONV_ROOT / "fixtures" / "dutylog"
REFERENCE = FIXTURE / "reference"
ALTERNATIVE = Path(__file__).resolve().parent / "fake" / "dutylog-alt"
CHEAT = Path(__file__).resolve().parent / "fake" / "dutylog-cheat"
MODULES = ("parsing", "validation", "aggregate", "report")
MODULE_GROUPS = MODULES
CHILD_ENV = {"PATH": os.environ.get("PATH", ""), "PYTHONDONTWRITEBYTECODE": "1"}


def build_cell(where):
    """A workspace and a judge directory, built by the fixture run.sh calls."""
    work, judge = Path(where) / "work", Path(where) / "judge"
    done = subprocess.run(
        ["bash", "-c",
         'source "$CONV_ROOT/fixtures/dutylog.sh"; fixture_dutylog "$1" "$2"',
         "fixture", str(work), str(judge)],
        env=dict(CHILD_ENV, CONV_ROOT=str(CONV_ROOT)), capture_output=True, text=True)
    if done.returncode != 0:
        raise AssertionError("the fixture did not generate: " + done.stderr)
    return work, judge


class Cell:
    """One prepared cell, cheap to copy for each way of repairing it."""

    def __init__(self, folder):
        self.folder = Path(folder)
        self.work, self.judge = build_cell(self.folder)


CELL = None


def setUpModule():
    global CELL
    CELL = Cell(tempfile.mkdtemp(prefix="dutylog-fixture-"))


def tearDownModule():
    shutil.rmtree(CELL.folder, ignore_errors=True)


class FixtureCase(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.project = Path(self.tmp.name) / "project"
        shutil.copytree(CELL.work, self.project)

    # ── the ways a project can arrive at the judge ─────────────────────────
    def repair(self, *modules, source=REFERENCE):
        for module in modules:
            shutil.copy(Path(source) / (module + ".py"), self.project / "dutylog")

    def judge(self, project=None):
        out = Path(self.tmp.name) / "verdict.json"
        done = subprocess.run(
            [sys.executable, str(CELL.judge / "judge_dutylog.py"),
             "--project", str(project or self.project), "--out", str(out)],
            capture_output=True, text=True, env=CHILD_ENV)
        self.assertEqual(done.returncode, 0, done.stderr)
        return json.loads(out.read_text()), out

    def visible_suite(self):
        return subprocess.run(
            [sys.executable, "-m", "unittest", "discover", "-s", "tests", "-t", "."],
            cwd=str(self.project), capture_output=True, text=True, env=CHILD_ENV)

    def failing_groups(self, verdict):
        return sorted(name for name, state in verdict["groups"].items() if not state["passed"])

    # ── red before ─────────────────────────────────────────────────────────
    def test_the_fixture_as_handed_over_fails_every_group(self):
        verdict, _ = self.judge()
        self.assertFalse(verdict["passed"])
        self.assertEqual(self.failing_groups(verdict),
                         sorted(("integration",) + MODULE_GROUPS))

    def test_the_visible_suite_names_all_four_modules_before_any_repair(self):
        done = self.visible_suite()
        self.assertNotEqual(done.returncode, 0, "the fixture arrives green")
        for module in MODULES:
            self.assertIn("tests.test_" + module, done.stderr,
                          "no visible failure points at " + module)
        self.assertIn("tests.test_pipeline", done.stderr)

    def test_each_defect_is_independent_of_the_others(self):
        # One module repaired fixes that module's group and no other. A defect
        # that healed when its neighbour was fixed would not be separate work.
        for module in MODULES:
            with self.subTest(module=module):
                shutil.rmtree(self.project)
                shutil.copytree(CELL.work, self.project)
                self.repair(module)
                verdict, _ = self.judge()
                self.assertTrue(verdict["groups"][module]["passed"],
                                verdict["groups"][module]["failed"])
                for other in MODULES:
                    if other != module:
                        self.assertFalse(verdict["groups"][other]["passed"],
                                         other + " healed when " + module + " was repaired")
                self.assertFalse(verdict["groups"]["integration"]["passed"])

    # ── green after ────────────────────────────────────────────────────────
    def test_the_reference_repair_passes_every_case(self):
        self.repair(*MODULES)
        verdict, _ = self.judge()
        self.assertTrue(verdict["passed"], verdict["groups"])
        self.assertEqual(verdict["failed"], 0)
        self.assertGreaterEqual(verdict["cases"], 30)

    def test_the_reference_repair_turns_the_visible_suite_green(self):
        self.repair(*MODULES)
        done = self.visible_suite()
        self.assertEqual(done.returncode, 0, done.stderr)

    def test_a_different_correct_implementation_also_passes(self):
        # The judge marks behaviour. A rewrite that shares no line with the
        # reference must pass, or the benchmark is marking style.
        self.repair(*MODULES, source=ALTERNATIVE)
        verdict, _ = self.judge()
        self.assertTrue(verdict["passed"], verdict["groups"])
        self.assertEqual(self.visible_suite().returncode, 0)

    # ── partial work is not finished work ──────────────────────────────────
    def test_three_modules_out_of_four_never_passes(self):
        for left_out in MODULES:
            with self.subTest(left_out=left_out):
                shutil.rmtree(self.project)
                shutil.copytree(CELL.work, self.project)
                self.repair(*[m for m in MODULES if m != left_out])
                verdict, _ = self.judge()
                self.assertFalse(verdict["passed"])
                self.assertFalse(verdict["groups"]["integration"]["passed"],
                                 "the pipeline passed without " + left_out)
                self.assertFalse(verdict["groups"][left_out]["passed"])

    # ── forgery ────────────────────────────────────────────────────────────
    def test_answers_hard_coded_to_the_visible_tests_do_not_pass(self):
        # Every module repaired except aggregate, which answers the two windows
        # the visible suite asks about from a table and is otherwise unchanged.
        self.repair("parsing", "validation", "report")
        self.repair("aggregate", source=CHEAT)
        self.assertEqual(self.visible_suite().returncode, 0,
                         "the counterexample must make the visible suite green")
        verdict, _ = self.judge()
        self.assertFalse(verdict["passed"])
        self.assertFalse(verdict["groups"]["aggregate"]["passed"])
        self.assertFalse(verdict["groups"]["integration"]["passed"])

    def test_a_planted_verdict_in_the_workspace_changes_nothing(self):
        (self.project / "judge-verdict.json").write_text(
            json.dumps({"passed": True, "groups": {}}))
        (self.project / "RESULT.txt").write_text("all tests pass\n")
        verdict, _ = self.judge()
        self.assertFalse(verdict["passed"])

    def test_a_planted_verdict_at_the_judges_own_path_is_overwritten(self):
        self.repair("parsing")
        out = Path(self.tmp.name) / "verdict.json"
        out.write_text(json.dumps({"passed": True, "groups": {}, "failed": 0}))
        verdict, path = self.judge()
        self.assertEqual(path, out)
        self.assertFalse(verdict["passed"])

    def test_a_deleted_module_is_a_verdict_and_not_a_crash(self):
        (self.project / "dutylog" / "aggregate.py").unlink()
        verdict, _ = self.judge()
        self.assertFalse(verdict["passed"])
        self.assertEqual(self.failing_groups(verdict),
                         sorted(("integration",) + MODULE_GROUPS))

    def test_the_judge_is_not_in_the_workspace(self):
        inside = {path.name for path in CELL.work.rglob("*")}
        self.assertNotIn("judge_dutylog.py", inside)
        self.assertNotIn("workspace-guard.json", inside)
        self.assertNotIn("judge-verdict.json", inside)
        for module in MODULES:
            self.assertFalse((CELL.work / "reference" / (module + ".py")).exists())
        self.assertTrue((CELL.judge / "judge_dutylog.py").is_file())


class GuardManifestCase(unittest.TestCase):
    """The checksum that tells "made the tests pass" from "made them agree"."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.project = Path(self.tmp.name) / "project"
        shutil.copytree(CELL.work, self.project)
        self.manifest = Path(self.tmp.name) / "guard.json"
        self.assertEqual(self.run_guard("create", str(self.project), str(self.manifest),
                                        "--path", "tests", "--path", "SPEC.md").returncode, 0)

    def run_guard(self, *arguments):
        return subprocess.run([sys.executable, str(FIXTURE / "manifest.py")] + list(arguments),
                              capture_output=True, text=True, env=CHILD_ENV)

    def verify(self):
        return self.run_guard("verify", str(self.project), str(self.manifest))

    def test_an_untouched_workspace_verifies(self):
        self.assertEqual(self.verify().returncode, 0, self.verify().stdout)

    def test_an_edited_test_is_caught(self):
        target = self.project / "tests" / "test_report.py"
        target.write_text(target.read_text().replace("assertEqual", "assertNotEqual"))
        done = self.verify()
        self.assertEqual(done.returncode, 1)
        self.assertIn("changed: tests/test_report.py", done.stdout)

    def test_a_deleted_test_is_caught(self):
        (self.project / "tests" / "test_pipeline.py").unlink()
        done = self.verify()
        self.assertEqual(done.returncode, 1)
        self.assertIn("missing: tests/test_pipeline.py", done.stdout)

    def test_an_edited_contract_is_caught(self):
        spec = self.project / "SPEC.md"
        spec.write_text(spec.read_text() + "\nAlso, everything is optional.\n")
        done = self.verify()
        self.assertEqual(done.returncode, 1)
        self.assertIn("changed: SPEC.md", done.stdout)

    def test_an_added_file_is_reported_and_is_not_a_failure(self):
        (self.project / "tests" / "test_scratch.py").write_text("# thinking out loud\n")
        done = self.verify()
        self.assertEqual(done.returncode, 0)
        self.assertIn("added: tests/test_scratch.py", done.stdout)

    def test_repairing_the_source_is_not_tampering(self):
        for module in MODULES:
            shutil.copy(REFERENCE / (module + ".py"), self.project / "dutylog")
        self.assertEqual(self.verify().returncode, 0)


if __name__ == "__main__":
    unittest.main()
