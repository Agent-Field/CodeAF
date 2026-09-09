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
import signal
import subprocess
import sys
import tempfile
import time
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

    def judge(self, project=None, budget=None, per_call=None, wall=120):
        out = Path(self.tmp.name) / "verdict.json"
        env = dict(CHILD_ENV)
        if budget is not None:
            env["DUTYLOG_BUDGET_S"] = str(budget)
        if per_call is not None:
            env["DUTYLOG_CLI_TIMEOUT_S"] = str(per_call)
        done = subprocess.run(
            [sys.executable, str(CELL.judge / "judge_dutylog.py"),
             "--project", str(project or self.project), "--out", str(out)],
            capture_output=True, text=True, env=env, timeout=wall)
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
        self.assertGreaterEqual(verdict["cases"], 20)
        for group in ("integration",) + MODULE_GROUPS:
            self.assertGreaterEqual(verdict["groups"][group]["cases"], 4, group)

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

    def test_a_project_that_hangs_on_import_is_a_verdict_within_the_budget(self):
        # Reported by root review: the judge used to import the candidate into
        # its own process, so this slept for as long as it liked.
        (self.project / "dutylog" / "__init__.py").write_text(
            "import time\ntime.sleep(600)\n")
        started = time.monotonic()
        verdict, _ = self.judge(budget=4, per_call=1, wall=60)
        self.assertLess(time.monotonic() - started, 45)
        self.assertFalse(verdict["passed"])
        self.assertTrue(any("killed" in failure["detail"] or "budget" in failure["detail"]
                            for state in verdict["groups"].values()
                            for failure in state["failed"]), verdict["groups"])

    # ── the lifecycle of the candidate's own processes ─────────────────────
    #
    # These stage a candidate CLI that leaves something behind, and each cleans
    # up after itself in a `finally` — a test of a process-killing helper must
    # not depend on that helper working to avoid leaking the process it staged.

    def plant_cli(self, body):
        (self.project / "dutylog" / "cli.py").write_text(body)

    def wait_for_pid_file(self, marker, within=20):
        for _ in range(int(within * 20)):
            if marker.exists() and marker.read_text().strip():
                return int(marker.read_text().strip())
            time.sleep(0.05)
        self.fail("the counterexample never recorded its descendant")

    def force_kill(self, pid):
        if pid is None:
            return
        for attempt in (signal.SIGKILL,):
            try:
                os.kill(pid, attempt)
            except (ProcessLookupError, PermissionError):
                return

    def assert_gone(self, pid, within=15):
        for _ in range(int(within * 20)):
            try:
                os.kill(pid, 0)
            except (ProcessLookupError, PermissionError):
                return
            time.sleep(0.05)
        self.fail("a process the judge started outlived it: pid %d" % pid)

    # A descendant written as its own file: the planted CLI starts it, and it
    # records its pid so the test can insist afterwards that it is gone.
    DESCENDANT = """import os, signal, time
{ignore}open({marker!r}, "w").write(str(os.getpid()))
time.sleep(600)
"""
    LEADER = """import subprocess, sys, time
from pathlib import Path
helper = str(Path(__file__).with_name("_descendant.py"))
marker = Path({marker!r})
{spawn}
for _ in range(500):  # do not exit before the descendant has named itself
    if marker.exists():
        break
    time.sleep(0.02)
{tail}
"""

    def plant_descendant(self, marker, ignore_term=True, quiet=False, leader_exits=False):
        (self.project / "dutylog" / "_descendant.py").write_text(self.DESCENDANT.format(
            ignore="signal.signal(signal.SIGTERM, signal.SIG_IGN)\n" if ignore_term else "",
            marker=str(marker)))
        spawn = ("quiet = open(os.devnull, 'w')\n"
                 "subprocess.Popen([sys.executable, helper], stdout=quiet, stderr=quiet)"
                 if quiet else "subprocess.Popen([sys.executable, helper])")
        self.plant_cli(self.LEADER.format(
            marker=str(marker),
            spawn=("import os\n" + spawn) if quiet else spawn,
            tail="raise SystemExit(0)" if leader_exits else "time.sleep(600)"))

    def test_a_descendant_that_ignores_term_and_holds_the_pipes_is_still_killed(self):
        # (a) The leader dies on TERM. Its child ignores TERM, inherited stdout,
        # and never closes it: waiting for the leader and then reading the pipe
        # is how this hangs forever.
        marker = Path(self.tmp.name) / "stubborn.pid"
        self.plant_descendant(marker)
        pid = None
        try:
            verdict, _ = self.judge(budget=4, per_call=1, wall=90)
            pid = self.wait_for_pid_file(marker)
            self.assertFalse(verdict["passed"])
            self.assert_gone(pid)
        finally:
            self.force_kill(pid)

    def test_a_descendant_of_a_leader_that_exited_normally_is_not_left_running(self):
        # (b) The CLI starts something with its own stdio, prints nothing and
        # exits 0. Nothing times out, so only the ordinary path can clean up.
        marker = Path(self.tmp.name) / "orphan.pid"
        self.plant_descendant(marker, ignore_term=False, quiet=True, leader_exits=True)
        pid = None
        try:
            verdict, _ = self.judge(wall=120)
            pid = self.wait_for_pid_file(marker)
            self.assertFalse(verdict["passed"], "a CLI that prints nothing cannot pass")
            self.assert_gone(pid)
        finally:
            self.force_kill(pid)

    def test_terminating_the_judge_takes_the_candidates_processes_with_it(self):
        # (c) What the scenario's own watchdog does. The judge is signalled; the
        # candidate's group is in a different session and would not hear it.
        marker = Path(self.tmp.name) / "signalled.pid"
        self.plant_descendant(marker)
        out = Path(self.tmp.name) / "verdict.json"
        judge = subprocess.Popen(
            [sys.executable, str(CELL.judge / "judge_dutylog.py"),
             "--project", str(self.project), "--out", str(out)],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
            env=dict(CHILD_ENV, DUTYLOG_BUDGET_S="120", DUTYLOG_CLI_TIMEOUT_S="90"))
        pid = None
        try:
            pid = self.wait_for_pid_file(marker)
            judge.terminate()
            judge.communicate(timeout=30)
            self.assertNotEqual(judge.returncode, 0, "a terminated judge did not report it")
            self.assertFalse(out.exists(), "a killed judge must not leave a verdict")
            self.assert_gone(pid)
        finally:
            if judge.poll() is None:
                judge.kill()
                judge.communicate(timeout=15)
            self.force_kill(pid)

    def test_a_command_that_hangs_is_killed_with_its_descendants(self):
        marker = Path(self.tmp.name) / "grandchild.pid"
        (self.project / "dutylog" / "cli.py").write_text(
            "import os, subprocess, sys, time\n"
            "child = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(600)'])\n"
            "open(%r, 'w').write(str(child.pid))\n"
            "time.sleep(600)\n" % str(marker))
        verdict, _ = self.judge(budget=4, per_call=1, wall=60)
        self.assertFalse(verdict["passed"])
        self.assertTrue(marker.exists(), "the counterexample never started its grandchild")
        pid = int(marker.read_text())
        for _ in range(50):  # the kill is signalled, not instantaneous
            try:
                os.kill(pid, 0)
            except (ProcessLookupError, PermissionError):
                break
            time.sleep(0.1)
        else:
            os.kill(pid, 9)
            self.fail("a grandchild of the judged CLI outlived the judge")

    def test_candidate_code_cannot_rewrite_the_judges_own_cases(self):
        # The exact counterexample from the root review: all four defects left
        # in place, and only dutylog/__init__.py changed, to replace the judge's
        # case bodies with no-ops. It passed 34 of 34 when the judge imported
        # the candidate. The judge is a parent now, and this is a child.
        (self.project / "dutylog" / "__init__.py").write_text(
            "import sys\n"
            "main = sys.modules.get('__main__')\n"
            "cases = getattr(main, 'CASES', None)\n"
            "if cases is not None:\n"
            "    main.CASES = [(group, name, lambda *a, **k: None) for group, name, _ in cases]\n"
            "    for group in getattr(main, 'GROUPS', ()):\n"
            "        pass\n")
        verdict, _ = self.judge()
        self.assertFalse(verdict["passed"], "the supplied forgery still passes")
        self.assertEqual(self.failing_groups(verdict),
                         sorted(("integration",) + MODULE_GROUPS))

    def test_the_judge_never_imports_the_candidate(self):
        source = (CELL.judge / "judge_dutylog.py").read_text()
        for forbidden in ("import_module", "importlib", "sys.path.insert"):
            self.assertNotIn(forbidden, source,
                             "the judge is back to loading candidate code in-process")

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
