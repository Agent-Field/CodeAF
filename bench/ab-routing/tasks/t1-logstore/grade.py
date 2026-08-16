#!/usr/bin/env python3
"""Grade a T1 (tinylog) submission. Deterministic; no LLM judge anywhere.

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
    1: "api: put/get/delete, binary safety, empty value is not a delete",
    2: "durability: data survives close and reopen",
    3: "scan: half-open bounds, byte-lexicographic order, deletes excluded",
    4: "recovery: torn or corrupt tail discarded and truncated off",
    5: "compact: merges every segment into one, drops tombstoned keys",
    6: "format: bytes on disk match the specified frame (independent parser)",
    7: "segments: the active segment rolls at the threshold",
    8: "manifest: a missing or corrupt manifest is rebuilt from the directory",
    9: "performance: 100k keys are indexed, not rescanned; scan is lazy",
}


def grade(workspace):
    result = {"task": "t1-logstore", "gates": {}, "groups": {}, "notes": [],
              "groups_total": len(GROUPS), "groups_passed": 0, "score": 0.0,
              "success": False}

    package_root = grouprun.find_package(workspace, "tinylog")
    if package_root is None:
        result["gates"]["package_present"] = False
        result["notes"].append("no importable tinylog/ package anywhere under the workspace")
        return result
    result["gates"]["package_present"] = True
    result["package_root"] = os.path.relpath(package_root, workspace)
    if result["package_root"] != ".":
        result["notes"].append(f"package found nested at {result['package_root']}")

    sandbox = grouprun.sandboxed(package_root, HIDDEN)
    try:
        imported = subprocess.run(
            [sys.executable, "-c", "import tinylog; tinylog.LogStore"],
            cwd=sandbox, capture_output=True, text=True, timeout=60,
            env={**os.environ, "PYTHONPATH": sandbox})
        result["gates"]["import"] = imported.returncode == 0
        if imported.returncode != 0:
            result["notes"].append(
                "import tinylog failed: " + imported.stderr.strip()[-400:])
            return result
        result["groups"] = grouprun.run_groups(sandbox, GROUPS)
    finally:
        shutil.rmtree(sandbox, ignore_errors=True)

    passed, raw, gated = grouprun.score_from(result["groups"], result["gates"])
    result["groups_passed"] = passed
    result["score"] = gated
    if gated != raw:
        result["score_before_gates"] = raw
    # A single number for the A/B table. tinylog is built from nothing, so
    # demanding all nine groups would floor both arms and measure nothing.
    # "success" is the point at which the thing is actually the store that was
    # asked for: it round-trips, survives a reopen, scans correctly, writes the
    # specified bytes, and is segmented with a manifest — which is what round 2
    # added and what round 1 could be passed without doing. Recovery,
    # compaction and the performance floor are the hard tail and are reported
    # separately rather than folded into a pass/fail.
    core = [result["groups"].get(str(n), {}).get("ok", False)
            for n in (1, 2, 3, 6, 7, 8)]
    result["success"] = bool(all(core)) and result["score"] > 0
    result["core_groups_passed"] = sum(core)
    return result


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("workspace")
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args()
    grouprun.emit(grade(args.workspace), args.json)


if __name__ == "__main__":
    main()
