#!/usr/bin/env python3
"""Grade a T4 (pathmatch) submission. Deterministic; no LLM judge.

Fourteen capability groups, each run in its own pytest process. The score is the
fraction of groups in which every test passes.

`success` is all fourteen. That is a high bar on purpose: t4 exists because t1
and t3 both floored at 0/3 in both arms, so what this task has to provide is a
*gradient* — the score, not the flag, is the measurement here.

Three families are reported apart, because the score alone does not say why:

  baseline (8)    ordinary glob behaviour any competent implementation gets
  divergence (4)  where this spec differs from the familiar tool — "read the
                  spec" versus "wrote a glob from memory"
  effort (2)      nested `**` backtracking and compiling patterns once, which
                  careful reading does not supply on its own

Usage: python3 grade.py <workspace-dir> [--json]
"""
import argparse
import os
import shutil
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", ".."))

import grouprun  # noqa: E402

HIDDEN = os.path.join(HERE, "hidden", "test_hidden.py")

GROUPS = {
    1: "literals and case",
    2: "`?` matches exactly one non-slash character",
    3: "`*` stays inside a segment",
    4: "`**` crosses segments, whole-segment only",
    5: "character classes and ranges",
    6: "escaping",
    7: "anchoring on any `/`",
    8: "compile of blanks, comments and errors",
    9: "DIVERGENCE: `[!…]` negates, `[^…]` is a literal caret",
    10: "DIVERGENCE: trailing `/` requires a directory path",
    11: "DIVERGENCE: anchored patterns first, then list order",
    12: "DIVERGENCE: select ordering and de-duplication",
    13: "nested `**` backtracks correctly",
    14: "patterns compiled once, not per path",
}

# The groups where this spec deliberately differs from gitignore. A submission
# that pattern-matched its way to the familiar answer passes the first eight and
# loses these, so tracking them apart is what makes the score diagnostic rather
# than merely a number.
DIVERGENCE = (9, 10, 11, 12)
# Groups that reading the spec carefully is not sufficient for: they need
# backtracking and they need the patterns compiled once. Round 1 had neither and
# arm A scored 12/12 twice.
EFFORT = (13, 14)


def grade(workspace):
    result = {"task": "t4-pathmatch", "gates": {}, "groups": {}, "notes": [],
              "groups_total": len(GROUPS), "groups_passed": 0, "score": 0.0,
              "success": False}

    package_root = grouprun.find_package(workspace, "pathmatch")
    if package_root is None:
        result["gates"]["package_present"] = False
        result["notes"].append("no importable pathmatch/ package anywhere under the workspace")
        return result
    result["gates"]["package_present"] = True
    result["package_root"] = os.path.relpath(package_root, workspace)
    if result["package_root"] != ".":
        result["notes"].append(f"package found nested at {result['package_root']}")

    sandbox = grouprun.sandboxed(package_root, HIDDEN)
    try:
        imported = subprocess.run(
            [sys.executable, "-c",
             "import pathmatch; pathmatch.matches, pathmatch.select, "
             "pathmatch.compile_pattern, pathmatch.PatternError"],
            cwd=sandbox, capture_output=True, text=True, timeout=60,
            env={**os.environ, "PYTHONPATH": sandbox})
        result["gates"]["import"] = imported.returncode == 0
        if imported.returncode != 0:
            result["notes"].append(
                "importing pathmatch failed: " + imported.stderr.strip()[-400:])
            return result
        result["groups"] = grouprun.run_groups(sandbox, GROUPS)
    finally:
        shutil.rmtree(sandbox, ignore_errors=True)

    passed, raw, gated = grouprun.score_from(result["groups"], result["gates"])
    result["groups_passed"] = passed
    result["score"] = gated
    if gated != raw:
        result["score_before_gates"] = raw
    result["success"] = bool(result["score"] >= 1.0)
    result["divergence_passed"] = sum(
        1 for n in DIVERGENCE if result["groups"].get(str(n), {}).get("ok"))
    result["baseline_passed"] = sum(
        1 for n in GROUPS if n not in DIVERGENCE and n not in EFFORT
        and result["groups"].get(str(n), {}).get("ok"))
    result["effort_passed"] = sum(
        1 for n in EFFORT if result["groups"].get(str(n), {}).get("ok"))
    return result


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("workspace")
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args()
    r = grade(args.workspace)
    grouprun.emit(r, args.json)
    if not args.json and r.get("groups"):
        print(f"  baseline {r['baseline_passed']}/8   "
              f"divergence {r['divergence_passed']}/4   "
              f"effort {r['effort_passed']}/2")


if __name__ == "__main__":
    main()
