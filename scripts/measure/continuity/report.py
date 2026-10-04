#!/usr/bin/env python3
"""report.py <run dir>... : one markdown table of PASS/FAIL per check for each run, plus the agent-continuity numbers.

A run dir is what scripts/continuity.py wrote (<out>/<kind>/): compare.json, repeats.json, b-answers.json, results.json.
A check reads PASS, FAIL, or DERIVED (a file the other machine makes again on open; its facts are checked elsewhere).
"""
import json
import os
import sys


def verdict(v):
    return "DERIVED" if v.get("derived") else ("PASS" if v.get("pass") else "FAIL")


def load(d, name):
    p = os.path.join(d, name)
    return json.load(open(p)) if os.path.exists(p) else {}


OPEN_ROWS = ("project_tasks", "folder:plandb.db", "folder:card.json", "folder:meta.json")   # made again when the chat opens


def recompute(d):
    """Run today's checks over the saved snapshots. What the move carried is read right after the take (before the chat is
    opened, since an opened chat writes lines of its own); the files the chat makes again on open are read after the open."""
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    import check
    a, took, opened = load(d, "a.json"), load(d, "b.json"), load(d, "b-open.json")
    if not (a and took):
        return
    merged = check.compare(a, took)
    if opened:
        after_open = check.compare(a, opened)
        merged.update({k: after_open[k] for k in OPEN_ROWS if k in after_open})
    with open(os.path.join(d, "compare.json"), "w") as f:
        json.dump(merged, f, indent=1)


def main(dirs):
    for d in dirs:
        recompute(d)
    runs = [(d, load(d, "compare.json"), load(d, "results.json"), load(d, "repeats.json")) for d in dirs]
    names = []
    for _, c, _, _ in runs:
        names += [k for k in c if k not in names]
    print("| check | " + " | ".join(os.path.basename(os.path.dirname(d.rstrip("/"))) + "/" + os.path.basename(d.rstrip("/")) for d, *_ in runs) + " |")
    print("|---|" + "---|" * len(runs))
    for k in names:
        print(f"| {k} | " + " | ".join(verdict(c[k]) if k in c else "-" for _, c, _, _ in runs) + " |")
    for label, key in (("take seconds", "take_s"), ("repeated calls (same tool+args)", "repeated_calls")):
        print(f"| {label} | " + " | ".join(str(r.get(key)) for _, _, r, _ in runs) + " |")
    print("| near-repeat calls | " + " | ".join(str(rep.get("near_count")) for *_, rep in runs) + " |")
    print("| dangling calls in B's transcript | " + " | ".join(str(len(rep.get("dangling_calls_in_b", []))) for *_, rep in runs) + " |")
    print("| answers correct | " + " | ".join(str(r.get("answers_ok")) for _, _, r, _ in runs) + " |")


if __name__ == "__main__":
    main(sys.argv[1:])
