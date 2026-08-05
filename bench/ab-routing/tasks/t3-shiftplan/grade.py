#!/usr/bin/env python3
"""Grade a T3 (shiftplan refactor/debug) submission. Deterministic; no judge.

Score is the fraction of **defect families** repaired, not the fraction of
assertions passed, so a family carrying five assertions does not outweigh one
carrying three. Each family is run in its own pytest process because
shiftplan.assign holds a module-level cache that lets an earlier test answer a
later one -- see the header of hidden/test_hidden.py.

Two gates sit above the score and zero it when they trip:

  * **import gate** -- the package must import. A submission that does not
    import scores 0 whatever it wrote.
  * **tamper gate** -- tests/test_visible.py must be byte-identical to what
    shipped. The brief tells the agent not to touch it; making the contract
    green by editing the contract is the cheapest wrong answer available and
    has to be caught rather than rewarded.

Usage: python3 grade.py <workspace-dir> [--json]
"""
import argparse
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
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

TIMEOUT_S = 120


def digest(path):
    with open(path, "rb") as f:
        return hashlib.sha256(f.read()).hexdigest()


def find_package(root):
    """Locate the directory that holds `shiftplan/`.

    The agent is told to work in the workspace root, but a real run sometimes
    nests the package one level down. Grading the run rather than the
    instruction-following means looking for it, and reporting where it was
    found so a nested layout is visible in the record rather than silently
    forgiven.
    """
    for depth, (dirpath, dirnames, _files) in enumerate(os.walk(root)):
        if os.path.basename(dirpath) in {".git", "__pycache__", ".venv", "obs"}:
            dirnames[:] = []
            continue
        if "shiftplan" in dirnames and os.path.isfile(
                os.path.join(dirpath, "shiftplan", "__init__.py")):
            return dirpath
        if depth > 200:
            break
    return None


def run_group(sandbox, group):
    cmd = [sys.executable, "-m", "pytest", "-q", "-p", "no:cacheprovider",
           "_hidden_test.py", "-k", f"GROUP_{group}_"]
    try:
        p = subprocess.run(cmd, cwd=sandbox, capture_output=True, text=True,
                           timeout=TIMEOUT_S,
                           env={**os.environ, "PYTHONDONTWRITEBYTECODE": "1"})
    except subprocess.TimeoutExpired:
        return {"passed": 0, "total": None, "ok": False, "note": "timeout"}
    tail = (p.stdout + p.stderr)[-4000:]
    passed = int((re.search(r"(\d+) passed", tail) or [0, 0])[1])
    failed = int((re.search(r"(\d+) failed", tail) or [0, 0])[1])
    errors = int((re.search(r"(\d+) error", tail) or [0, 0])[1])
    total = passed + failed + errors
    return {"passed": passed, "total": total,
            "ok": failed == 0 and errors == 0 and passed > 0,
            "note": "" if total else tail[-600:]}


def grade(workspace):
    result = {"task": "t3-shiftplan", "gates": {}, "groups": {},
              "families_fixed": 0, "families_total": len(GROUPS),
              "score": 0.0, "notes": []}

    pkg_root = find_package(workspace)
    if pkg_root is None:
        result["gates"]["import"] = False
        result["notes"].append("no shiftplan/ package found anywhere under the workspace")
        return result
    result["package_root"] = os.path.relpath(pkg_root, workspace)
    if result["package_root"] != ".":
        result["notes"].append(f"package found nested at {result['package_root']}")

    # tamper gate
    submitted_visible = os.path.join(pkg_root, "tests", "test_visible.py")
    if not os.path.isfile(submitted_visible):
        result["gates"]["visible_intact"] = False
        result["notes"].append("tests/test_visible.py is missing — the contract was deleted")
    else:
        same = digest(submitted_visible) == digest(VISIBLE_REF)
        result["gates"]["visible_intact"] = same
        if not same:
            result["notes"].append("tests/test_visible.py was modified — the contract was edited")

    with tempfile.TemporaryDirectory() as sandbox:
        for entry in os.listdir(pkg_root):
            if entry in {".git", "__pycache__", ".venv", ".pytest_cache"}:
                continue
            src = os.path.join(pkg_root, entry)
            dst = os.path.join(sandbox, entry)
            (shutil.copytree if os.path.isdir(src) else shutil.copy2)(src, dst)
        shutil.copy2(HIDDEN, os.path.join(sandbox, "_hidden_test.py"))

        imported = subprocess.run(
            [sys.executable, "-c", "import shiftplan"], cwd=sandbox,
            capture_output=True, text=True, timeout=60)
        result["gates"]["import"] = imported.returncode == 0
        if imported.returncode != 0:
            result["notes"].append(
                "import shiftplan failed: " + imported.stderr.strip()[-400:])
            return result

        # the shipped suite must still be green: a fix that breaks what worked
        # is not a fix
        vis = subprocess.run(
            [sys.executable, "-m", "pytest", "-q", "-p", "no:cacheprovider",
             "tests/test_visible.py"],
            cwd=sandbox, capture_output=True, text=True, timeout=TIMEOUT_S)
        vtail = (vis.stdout + vis.stderr)[-3000:]
        result["visible_passed"] = int((re.search(r"(\d+) passed", vtail) or [0, 0])[1])
        result["visible_failed"] = int((re.search(r"(\d+) failed", vtail) or [0, 0])[1])

        for g, label in GROUPS.items():
            r = run_group(sandbox, g)
            r["label"] = label
            result["groups"][str(g)] = r

    result["families_fixed"] = sum(1 for r in result["groups"].values() if r["ok"])
    result["score"] = round(result["families_fixed"] / len(GROUPS), 4)
    if not result["gates"].get("import", False) or \
            not result["gates"].get("visible_intact", False):
        result["score_before_gates"] = result["score"]
        result["score"] = 0.0
        result["notes"].append("score zeroed by a gate")
    # "success" for the A/B table is a high bar deliberately: the brief asks
    # for every defect family and the refactor, so anything less is a partial.
    result["success"] = bool(result["score"] >= 1.0)
    return result


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("workspace")
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args()
    r = grade(args.workspace)
    if args.json:
        print(json.dumps(r))
        return
    print(f"t3-shiftplan  score {r['score']:.3f}  "
          f"({r['families_fixed']}/{r['families_total']} families)  "
          f"success={r['success']}")
    for g, gr in sorted(r["groups"].items()):
        mark = "PASS" if gr["ok"] else "fail"
        print(f"  group {g}  {mark}  {gr['passed']}/{gr['total']}  {gr['label']}")
    for n in r["notes"]:
        print(f"  ! {n}")


if __name__ == "__main__":
    main()
