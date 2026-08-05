"""Shared machinery for the two code-graded tasks (T1, T3).

Both grade a Python package an agent left on disk by running a hidden suite
against it, and both score **capability groups** rather than assertions, so a
group carrying six assertions does not outweigh one carrying three.

Two decisions live here because both tasks need them and getting either wrong
silently corrupts a score:

  * **one pytest process per group.** Both submissions are stateful — tinylog
    holds open file handles, shiftplan holds a module-level cache — and in one
    process an earlier test can answer a later one. The first draft of the T3
    suite scored a defect family 3/3 against the *buggy* package for exactly
    this reason.
  * **find the package rather than assume it.** Agents sometimes nest their
    work one directory down. Grading the instruction rather than the work would
    turn a layout quirk into a capability result, so the package is located and
    the location recorded.
"""

import json
import os
import re
import shutil
import subprocess
import sys
import tempfile

TIMEOUT_S = 180
SKIP_DIRS = {".git", "__pycache__", ".venv", ".pytest_cache", "obs",
             "node_modules", ".aforge"}


def find_package(root, package):
    """Return the directory containing `package/__init__.py`, shallowest first."""
    best = None
    best_depth = 10 ** 9
    root = os.path.abspath(root)
    for dirpath, dirnames, _files in os.walk(root):
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS]
        if os.path.isfile(os.path.join(dirpath, package, "__init__.py")):
            depth = os.path.relpath(dirpath, root).count(os.sep)
            if os.path.relpath(dirpath, root) == ".":
                depth = -1
            if depth < best_depth:
                best, best_depth = dirpath, depth
    return best


def stage(package_root, hidden_test, sandbox):
    """Copy the submission plus the hidden suite into a scratch directory.

    Grading in a copy rather than in place means a test that writes files
    cannot change what the next group sees, and the run's own workspace is left
    exactly as the agent left it for later inspection.
    """
    for entry in os.listdir(package_root):
        if entry in SKIP_DIRS:
            continue
        src = os.path.join(package_root, entry)
        dst = os.path.join(sandbox, entry)
        try:
            if os.path.isdir(src):
                shutil.copytree(src, dst,
                                ignore=shutil.ignore_patterns(*SKIP_DIRS))
            else:
                shutil.copy2(src, dst)
        except OSError:
            pass
    shutil.copy2(hidden_test, os.path.join(sandbox, "_hidden_test.py"))


def _counts(text):
    def n(pattern):
        m = re.search(pattern, text)
        return int(m.group(1)) if m else 0
    return n(r"(\d+) passed"), n(r"(\d+) failed"), n(r"(\d+) error")


def pytest_in(sandbox, *args):
    env = {**os.environ, "PYTHONDONTWRITEBYTECODE": "1", "PYTHONPATH": sandbox}
    try:
        p = subprocess.run(
            [sys.executable, "-m", "pytest", "-q", "-p", "no:cacheprovider",
             "--timeout", "60", *args] if _has_timeout_plugin() else
            [sys.executable, "-m", "pytest", "-q", "-p", "no:cacheprovider",
             *args],
            cwd=sandbox, capture_output=True, text=True, timeout=TIMEOUT_S,
            env=env)
    except subprocess.TimeoutExpired:
        return 0, 0, 0, "timed out"
    tail = (p.stdout + p.stderr)[-6000:]
    passed, failed, errors = _counts(tail)
    return passed, failed, errors, tail


_TIMEOUT_PLUGIN = None


def _has_timeout_plugin():
    global _TIMEOUT_PLUGIN
    if _TIMEOUT_PLUGIN is None:
        try:
            import pytest_timeout  # noqa: F401
            _TIMEOUT_PLUGIN = True
        except Exception:
            _TIMEOUT_PLUGIN = False
    return _TIMEOUT_PLUGIN


def run_groups(sandbox, groups):
    out = {}
    for number, label in groups.items():
        passed, failed, errors, tail = pytest_in(
            sandbox, "_hidden_test.py", "-k", f"GROUP_{number}_")
        total = passed + failed + errors
        out[str(number)] = {
            "label": label, "passed": passed, "failed": failed,
            "errors": errors, "total": total,
            "ok": bool(total and failed == 0 and errors == 0),
            # only the first failure line is kept: the point of the record is
            # to say *how* a group failed, not to reproduce a pytest log
            "first_failure": _first_failure(tail),
        }
    return out


def _first_failure(tail):
    for line in tail.splitlines():
        if line.startswith("FAILED ") or line.startswith("ERROR "):
            return line.strip()[:200]
    if "timed out" in tail:
        return "timed out"
    return ""


def score_from(groups, gates):
    fixed = sum(1 for g in groups.values() if g["ok"])
    total = len(groups) or 1
    raw = round(fixed / total, 4)
    gated = 0.0 if not all(gates.values()) else raw
    return fixed, raw, gated


def emit(result, as_json):
    if as_json:
        print(json.dumps(result))
        return
    print(f"{result['task']}  score {result['score']:.3f}  "
          f"({result['groups_passed']}/{result['groups_total']} groups)  "
          f"success={result['success']}")
    for number, g in sorted(result["groups"].items(), key=lambda kv: int(kv[0])):
        mark = "PASS" if g["ok"] else "fail"
        print(f"  group {number}  {mark}  {g['passed']}/{g['total']}  {g['label']}")
        if g["first_failure"]:
            print(f"           {g['first_failure']}")
    for gate, ok in result["gates"].items():
        if not ok:
            print(f"  ! gate failed: {gate}")
    for note in result["notes"]:
        print(f"  ! {note}")


def sandboxed(package_root, hidden_test):
    d = tempfile.mkdtemp(prefix="abgrade-")
    stage(package_root, hidden_test, d)
    return d
