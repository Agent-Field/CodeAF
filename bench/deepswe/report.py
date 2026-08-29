#!/usr/bin/env python3
"""Render the result directories the DeepSWE rig wrote.

    bench/deepswe/report.py [<result-dir> ...]

With no argument it reads every directory under results/. Reward is the
verifier's own binary score; `partial` is the fraction of whitelisted tests that
passed and is the number that tells a near-miss apart from a run that shipped
nothing. A missing reward.json is printed as `rig` — an infrastructure failure
is not a score of zero and must never be averaged in as one.
"""
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
RESULTS = os.environ.get("RESULTS", os.path.join(HERE, "results"))


_MISSING = object()


def read(path, default=_MISSING):
    """Load a JSON artifact, or hand back the caller's stand-in when it is not
    there. The stand-in is compared by identity, not truthiness: a missing
    reward.json has to stay distinguishable from an empty one, because one is a
    rig failure and the other is a score."""
    try:
        with open(path) as fh:
            return json.load(fh)
    except Exception:
        return {} if default is _MISSING else default


def row(d):
    meta = read(os.path.join(d, "meta.json"))
    cost = read(os.path.join(d, "cost.json"))
    reward = read(os.path.join(d, "reward.json"), default=None)
    note = ""
    if meta.get("void"):
        # A specialist worker took the run over, so it is not a measurement of
        # the path under test. Never averaged in, never silently dropped.
        return {"dir": os.path.basename(d), "task": meta.get("task", "?"),
                "lang": meta.get("language", ""), "reward": "VOID", "partial": "",
                "cost": read(os.path.join(d, "cost.json")).get("cost_usd", 0.0) or 0.0,
                "wall": meta.get("agent_seconds", 0), "exit": meta.get("exit_code", ""),
                "note": "ran on worker " + ",".join(meta.get("workers_ran", []))}
    if reward is None:
        score, partial = "rig", ""
        note = meta.get("stage", "?")
    else:
        score = str(reward.get("reward", "?"))
        partial = f"{reward.get('partial', 0):.3f}"
        if reward.get("apply_failed"):
            note = "patch did not apply"
        elif not meta.get("patch_bytes"):
            note = "empty patch"
        else:
            note = (f"f2p {reward.get('f2p_passed', 0)}/{reward.get('f2p_total', 0)}  "
                    f"p2p {reward.get('p2p_passed', 0)}/{reward.get('p2p_total', 0)}")
    return {
        "dir": os.path.basename(d),
        "task": meta.get("task", os.path.basename(d)),
        "lang": meta.get("language", ""),
        "reward": score,
        "partial": partial,
        "cost": cost.get("cost_usd", 0.0) or 0.0,
        "wall": meta.get("agent_seconds", 0),
        "exit": meta.get("exit_code", ""),
        "note": note,
    }


def main(dirs):
    if not dirs:
        # gold/ directories are the control, not a measurement — gold.sh reports
        # them, and averaging them in here would flatter every sweep by five
        # guaranteed ones. Name one explicitly to see it.
        dirs = [os.path.join(RESULTS, e) for e in sorted(os.listdir(RESULTS))
                if os.path.isdir(os.path.join(RESULTS, e)) and not e.endswith("-gold")]
    rows = [row(d) for d in dirs]
    print(f"{'TASK':46} {'LANG':11} {'REWARD':>6} {'PARTIAL':>8} {'COST$':>8} "
          f"{'WALL(s)':>8} {'EXIT':>5}  NOTE")
    print("-" * 128)
    solved = graded = 0
    total = 0.0
    for r in rows:
        if r["reward"] in ("0", "1"):
            graded += 1
            solved += r["reward"] == "1"
        total += r["cost"]
        print(f"{r['task'][:46]:46} {r['lang'][:11]:11} {r['reward']:>6} {r['partial']:>8} "
              f"{r['cost']:>8.3f} {str(r['wall']):>8} {str(r['exit']):>5}  {r['note'][:44]}")
    print()
    print(f"solved {solved}/{graded} graded; {len(rows) - graded} did not grade; "
          f"total ${total:.2f}")


if __name__ == "__main__":
    main(sys.argv[1:])
