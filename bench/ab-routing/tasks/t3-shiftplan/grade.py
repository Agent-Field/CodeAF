#!/usr/bin/env python3
"""Grade a T3 (shiftplan refactor/debug) submission. Deterministic; no judge.

Score is the fraction of **defect families** repaired, not the fraction of
assertions passed, so a family carrying five assertions does not outweigh one
carrying three. Two gates sit above the score and zero it when they trip:

  * **import gate** -- the package must import. A submission that does not
    import scores nothing whatever it wrote.
  * **tamper gate** -- tests/test_visible.py must be byte-identical to what
    shipped. Making the contract green by editing the contract is the cheapest
    wrong answer available and has to be caught rather than rewarded.

Usage: python3 grade.py <workspace-dir> [--json]
"""
import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", ".."))

import grouprun  # noqa: E402

HIDDEN = os.path.join(HERE, "hidden", "test_hidden.py")
VISIBLE_REF = os.path.join(HERE, "seed", "tests", "test_visible.py")

GROUPS = {
    1: "interval: half-open [start, end) semantics",
    2: "payroll: exact whole cents, half-up, no float drift",
    3: "defaults: no shared mutable state between calls",
    4: "cache: key covers every argument that changes the answer",
    5: "dates: bookings are scoped to a date",
    6: "loader: malformed input raises, never returns an empty roster",
    7: "strategy: the required pluggable-strategy refactor",
}


def digest(path):
    with open(path, "rb") as f:
        return hashlib.sha256(f.read()).hexdigest()


def grade(workspace):
    result = {"task": "t3-shiftplan", "gates": {}, "groups": {}, "notes": [],
              "groups_total": len(GROUPS), "groups_passed": 0, "score": 0.0,
              "success": False}

    package_root = grouprun.find_package(workspace, "shiftplan")
    if package_root is None:
        result["gates"]["package_present"] = False
        result["notes"].append("no importable shiftplan/ package anywhere under the workspace")
        return result
    result["gates"]["package_present"] = True
    result["package_root"] = os.path.relpath(package_root, workspace)
    if result["package_root"] != ".":
        result["notes"].append(f"package found nested at {result['package_root']}")

    submitted_visible = os.path.join(package_root, "tests", "test_visible.py")
    if not os.path.isfile(submitted_visible):
        result["gates"]["visible_intact"] = False
        result["notes"].append("tests/test_visible.py is missing — the contract was deleted")
    else:
        intact = digest(submitted_visible) == digest(VISIBLE_REF)
        result["gates"]["visible_intact"] = intact
        if not intact:
            result["notes"].append("tests/test_visible.py was modified — the contract was edited")

    sandbox = grouprun.sandboxed(package_root, HIDDEN)
    try:
        imported = subprocess.run(
            [sys.executable, "-c", "import shiftplan"], cwd=sandbox,
            capture_output=True, text=True, timeout=60,
            env={**os.environ, "PYTHONPATH": sandbox})
        result["gates"]["import"] = imported.returncode == 0
        if imported.returncode != 0:
            result["notes"].append(
                "import shiftplan failed: " + imported.stderr.strip()[-400:])
            return result

        # The shipped suite must still be green: a repair that breaks what
        # already worked is not a repair. Recorded, not gated -- the gate is on
        # the file being unedited, and a genuine regression is more legible as
        # a number than as a zero.
        passed, failed, _errors, _tail = grouprun.pytest_in(
            sandbox, "tests/test_visible.py")
        result["visible_passed"], result["visible_failed"] = passed, failed

        result["groups"] = grouprun.run_groups(sandbox, GROUPS)
    finally:
        shutil.rmtree(sandbox, ignore_errors=True)

    fixed, raw, gated = grouprun.score_from(result["groups"], result["gates"])
    result["groups_passed"] = fixed
    result["score"] = gated
    if gated != raw:
        result["score_before_gates"] = raw
    # The brief asks for every defect family and the refactor, so success is
    # all seven. Anything less is a partial and shows up in the score.
    result["success"] = bool(result["score"] >= 1.0)
    return result


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("workspace")
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args()
    grouprun.emit(grade(args.workspace), args.json)


if __name__ == "__main__":
    main()
