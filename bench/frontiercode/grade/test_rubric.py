#!/usr/bin/env python3
"""Focused regression tests for the rubric engine's grading defects.

These are the observable tests for the fixes in this change, and they run with
only the standard library — no container, no network, no provider key:

  * overlay application survives the context drift an agent's own nearby test
    creates (the conflicted-files-refname-crash s1-s3 shape), without changing
    the test's bytes;
  * a patch that does not apply grades a legitimate 0 instead of raising
    UnboundLocalError and leaving phase A with no verdicts;
  * a classical criterion whose overlay conflicted is answered by the adaptive
    path's phase B verdict, so the adaptive path can rescue a run;
  * scope path prefixes mean what the author wrote ('' / '.' / './' is no
    restriction; './src/' and 'src/' are the same).

    python3 grade/test_rubric.py
"""

import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import rubric  # noqa: E402


BASE_TEST = """\
def test_a():
    assert True


ANCHOR = 1
MID = 2
TAIL = 3
"""

# The agent's own test is long enough that it displaces the overlay's trailing
# context beyond git apply's search window — the conflicted-files shape, where
# the agent added a full test where the reference added its own.
AGENT_BLOCK = "def test_agent():\n" + "".join(
    f"    step_{i} = {i}\n" for i in range(25)
) + "\n\nANCHOR = 1"


def git(repo, *args):
    return subprocess.run(
        ["git", "-C", str(repo), *args],
        check=True, capture_output=True, text=True,
    )


def write(path, text):
    pathlib.Path(path).write_text(text)


class OverlayRepo:
    """A tiny git repo with a base test file, a reference overlay that inserts
    a test at an anchor, and an agent patch that inserts a different test at the
    same anchor — the exact shape that broke conflicted-files-refname-crash."""

    def __init__(self, root):
        self.repo = pathlib.Path(root) / "repo"
        self.repo.mkdir()
        git(self.repo, "init", "-q")
        git(self.repo, "config", "user.email", "t@example.invalid")
        git(self.repo, "config", "user.name", "T")
        self.test_file = self.repo / "tests" / "git_test.py"
        self.test_file.parent.mkdir()
        write(self.test_file, BASE_TEST)
        git(self.repo, "add", "-A")
        git(self.repo, "commit", "-q", "-m", "base")
        self.base = git(self.repo, "rev-parse", "HEAD").stdout.strip()

        # The reference overlay: a test inserted between test_a and ANCHOR.
        write(self.test_file, BASE_TEST.replace(
            "ANCHOR = 1",
            "def test_reference():\n    assert True\n\n\nANCHOR = 1",
        ))
        git(self.repo, "add", "-A")
        git(self.repo, "commit", "-q", "-m", "reference")
        self.overlay = pathlib.Path(root) / "overlay.patch"
        write(self.overlay, git(
            self.repo, "diff", self.base, "HEAD", "--", "tests/git_test.py",
        ).stdout)
        git(self.repo, "checkout", "-q", self.base)

        # The agent's patch: a differently-named test at the same anchor.
        write(self.test_file, BASE_TEST.replace("ANCHOR = 1", AGENT_BLOCK))
        git(self.repo, "add", "-A")
        git(self.repo, "commit", "-q", "-m", "agent")
        self.agent_patch = pathlib.Path(root) / "agent.patch"
        write(self.agent_patch, git(
            self.repo, "diff", self.base, "HEAD", "--", "tests/git_test.py",
        ).stdout)
        git(self.repo, "checkout", "-q", self.base)

    def apply_agent(self):
        code, out = rubric.sh(
            f"git apply --binary --whitespace=nowarn '{self.agent_patch}'",
            cwd=self.repo,
        )
        assert code == 0, out


class OverlayApplyTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.r = OverlayRepo(self.tmp.name)

    def test_exact_apply_on_pristine_base(self):
        ok, note = rubric.apply_overlay_idempotent(self.r.repo, str(self.r.overlay))
        self.assertTrue(ok, note)
        self.assertEqual(note, "applied")

    def test_reverse_check_when_already_in_tree(self):
        # A gold tree already carries the reference test.
        code, out = rubric.sh(f"git apply --whitespace=nowarn '{self.r.overlay}'",
                              cwd=self.r.repo)
        self.assertEqual(code, 0, out)
        ok, note = rubric.apply_overlay_idempotent(self.r.repo, str(self.r.overlay))
        self.assertTrue(ok, note)
        self.assertIn("already in the tree", note)

    def test_context_drift_applies_with_reduced_context(self):
        # The agent inserted its own test at the overlay's anchor. The exact
        # apply fails; the reduced-context apply must place the same bytes and
        # the reference test must be present afterwards.
        self.r.apply_agent()
        exact, _ = rubric.sh(f"git apply --check --whitespace=nowarn '{self.r.overlay}'",
                             cwd=self.r.repo)
        self.assertNotEqual(exact, 0, "expected the exact apply to conflict")
        ok, note = rubric.apply_overlay_idempotent(self.r.repo, str(self.r.overlay))
        self.assertTrue(ok, note)
        self.assertIn("reduced context", note)
        tree = (self.r.repo / "tests" / "git_test.py").read_text()
        self.assertIn("def test_reference()", tree)
        self.assertIn("def test_agent()", tree)

    def test_genuine_conflict_is_reported_not_applied(self):
        # An overlay whose context is gone cannot be placed at any tolerance.
        write(self.r.test_file, "totally different\ncontent\nhere\n")
        ok, note = rubric.apply_overlay_idempotent(self.r.repo, str(self.r.overlay))
        self.assertFalse(ok)
        self.assertTrue(note.startswith("overlay conflict"), note)


class PhaseATests(unittest.TestCase):
    """phase_a on a patch that cannot apply: a legitimate 0, never a crash."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)
        self.r = OverlayRepo(self.tmp.name)
        self.task = self.root / "task"
        self.task.mkdir()
        # One classical criterion (never reached) and one scope criterion.
        write(self.task / "rubric.toml", f"""\
schema_version = 1
judge_model = "test/model"
judge_prompt_version = "fc-judge-1"

[[criterion]]
id = "reference-tests-pass"
kind = "classical"
blocker = false
weight = 1
[criterion.classical]
overlay = "{self.r.overlay}"
run = "true"

[[criterion]]
id = "scope-focused"
kind = "scope"
blocker = false
weight = 1
[criterion.scope]
max_files = 2
max_changed_lines = 10
allowed_paths = ["tests/"]
""")
        # A patch that cannot apply to the pristine base.
        bad = self.root / "bad.patch"
        write(bad, "diff --git a/nope.txt b/nope.txt\n"
                   "--- a/nope.txt\n+++ b/nope.txt\n"
                   "@@ -1 +1 @@\n-absent\n+present\n")
        self.bad = bad

    def test_unappliable_patch_writes_verdicts(self):
        old = os.environ.get("FC_PATCH")
        os.environ["FC_PATCH"] = str(self.bad)
        self.addCleanup(lambda: os.environ.__setitem__("FC_PATCH", old) if old else os.environ.pop("FC_PATCH", None))
        out = self.root / "grade"
        rc = rubric.phase_a(str(self.task), str(self.r.repo), self.r.base, str(out))
        self.assertEqual(rc, 0)
        pa = json.loads((out / "phaseA.json").read_text())
        self.assertFalse(pa["apply_ok"])
        self.assertIn("judge_input", pa)
        self.assertEqual(pa["judge_input"]["overlay"], "")
        self.assertEqual(pa["criteria"]["reference-tests-pass"]["status"], rubric.FAIL)
        self.assertIn("patch did not apply", pa["criteria"]["reference-tests-pass"]["note"])
        self.assertTrue((out / "apply.txt").exists())


class CombineTests(unittest.TestCase):
    """A conflicted classical criterion is answered by phase B."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)
        self.task = self.root / "task"
        self.task.mkdir()
        write(self.task / "rubric.toml", """\
schema_version = 1
judge_model = "test/model"
judge_prompt_version = "fc-judge-1"

[[criterion]]
id = "reference-tests-pass"
kind = "classical"
blocker = true
weight = 1
[criterion.classical]
overlay = "overlay.patch"
run = "true"

[[criterion]]
id = "adapted-tests-pass"
kind = "adaptive-classical"
blocker = false
weight = 1
[criterion.adaptive_classical]
overlay = "overlay.patch"
run = "true"
""")
        self.grade = self.root / "grade"
        self.grade.mkdir()
        write(self.grade / "phaseA.json", json.dumps({
            "apply_ok": True,
            "apply_note": "",
            "judge_input": {"overlay": "", "criteria": {}, "test_files": {}, "base": "x"},
            "criteria": {
                "reference-tests-pass": {
                    "status": "rig",
                    "note": "test overlay conflict — adaptive path: overlay conflict: x",
                },
                "adapted-tests-pass": {"status": "host", "note": "answered on the host"},
            },
        }))
        write(self.grade / "judge.json", json.dumps({
            "adaptive": {"adapted": True, "reasoning": "rewrote the test"},
            "criteria": {},
        }))
        write(self.grade / "phaseB.json", json.dumps({
            "criteria": {"adapted-tests-pass": {
                "status": "pass", "exit_code": 0, "note": "adapted tests exit 0",
            }},
        }))

    def test_phase_b_resolves_the_conflicted_classical_criterion(self):
        grade = rubric.combine(str(self.task), str(self.grade))
        self.assertEqual(grade["criteria"]["reference-tests-pass"]["status"], rubric.PASS)
        self.assertEqual(grade["criteria"]["reference-tests-pass"]["source"], "phase-b")
        self.assertEqual(grade["criteria"]["adapted-tests-pass"]["status"], rubric.PASS)
        self.assertEqual(grade["rig_criteria"], [])
        self.assertEqual(grade["score"], 1.0)
        self.assertTrue(grade["pass"])

    def test_no_phase_b_keeps_the_classical_criterion_rig(self):
        (self.grade / "phaseB.json").unlink()
        write(self.grade / "judge.json", json.dumps({
            "adaptive": {"adapted": True, "reasoning": "rewrote the test"},
            "criteria": {},
        }))
        grade = rubric.combine(str(self.task), str(self.grade))
        self.assertEqual(grade["criteria"]["reference-tests-pass"]["status"], rubric.RIG)
        self.assertIsNone(grade["score"])
        self.assertEqual(grade["status"], "rig")


class ScopePrefixTests(unittest.TestCase):
    def _check(self, allowed, forbidden, files):
        patch = self.root / "p.patch"
        write(patch, "".join(
            f"diff --git a/{f} b/{f}\n--- a/{f}\n+++ b/{f}\n@@ -1 +1 @@\n-a\n+b\n"
            for f in files
        ))
        return rubric.scope_check(str(patch), {
            "max_files": 10, "max_changed_lines": 100,
            "allowed_paths": allowed, "forbidden_paths": forbidden,
        }, str(self.root))

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)

    def test_dot_slash_is_no_restriction(self):
        passed, reasons, _ = self._check(["./"], [], ["src/a.py"])
        self.assertTrue(passed, reasons)

    def test_empty_prefix_is_no_restriction(self):
        passed, reasons, _ = self._check([""], [], ["anything/a.py"])
        self.assertTrue(passed, reasons)

    def test_real_prefix_restricts(self):
        passed, reasons, _ = self._check(["src/"], [], ["other/a.py"])
        self.assertFalse(passed)
        self.assertIn("out-of-scope", " ".join(reasons))

    def test_dot_slash_prefix_normalized(self):
        passed, reasons, _ = self._check(["./src/"], [], ["src/a.py"])
        self.assertTrue(passed, reasons)

    def test_empty_forbidden_prefix_is_not_forbid_all(self):
        passed, reasons, _ = self._check([], [""], ["src/a.py"])
        self.assertTrue(passed, reasons)


if __name__ == "__main__":
    unittest.main(verbosity=2)