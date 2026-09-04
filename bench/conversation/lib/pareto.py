#!/usr/bin/env python3
"""Read a run's cells and say, per workload, which arms are not dominated.

WHY THIS IS NOT A LEADERBOARD. Three quantities matter at once — what a cell
cost, how long it took, and whether it actually did the job — and they trade
against each other. One arm being cheaper and another being better is the normal
result, not a tie to be broken. So this prints, for each workload separately,
the set of arms that nothing else beats on all three at once, and it does not
add the workloads together. A single number across coding, research, writing and
conversation would be an average over incomparable things.

WHAT IS EXCLUDED, AND LOUDLY. A cell counts here only if it ran under conditions
comparable to the cells beside it: same door, same effort rung, a billed model
its receipts could name, and a cost the harness reported. Everything else is
listed by name under "excluded" with the reason. An excluded cell is not a
passing cell and not a failing one — it is a measurement that was not made.

Quality is the fraction of the cell's own assertions that passed, so a cell that
got three of four right is not the same as one that got none. A cell that ran no
assertions at all has no quality figure and is excluded.
"""
import argparse
import collections
import json
import sys


def load(paths):
    cells = []
    for path in paths:
        with open(path, errors="replace") as handle:
            for line in handle:
                line = line.strip()
                if not line:
                    continue
                try:
                    cells.append(json.loads(line))
                except ValueError:
                    print("skipping a line that is not JSON in %s" % path, file=sys.stderr)
    return cells


def quality(cell):
    checks = cell.get("checks") or []
    if not checks:
        return None
    passed = sum(1 for check in checks if check.get("outcome") == "pass")
    return passed / len(checks)


def usable(cell):
    """Say whether a cell may be compared, and why not when it may not."""
    if cell.get("verdict") in ("unsupported", "skipped"):
        return False, cell.get("verdict") + ": " + (cell.get("reason") or "no reason recorded")
    if cell.get("comparable") != "yes":
        return False, "not comparable: " + (cell.get("reason") or "no reason recorded")
    if cell.get("cost_usd") is None:
        return False, "cost was not self-reported"
    if quality(cell) is None:
        return False, "the cell made no assertions"
    return True, ""


def dominates(a, b):
    """a dominates b when it is at least as good on all three and better on one.
    Quality is maximised; cost and wall clock are minimised."""
    at_least = (a["quality"] >= b["quality"] and a["cost"] <= b["cost"] and a["wall"] <= b["wall"])
    strictly = (a["quality"] > b["quality"] or a["cost"] < b["cost"] or a["wall"] < b["wall"])
    return at_least and strictly


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("results", nargs="+", help="one or more results.jsonl files")
    args = parser.parse_args()

    cells = load(args.results)
    if not cells:
        print("no cells to read")
        return 1

    excluded = []
    rolled = collections.defaultdict(list)
    for cell in cells:
        ok, why = usable(cell)
        if not ok:
            excluded.append((cell.get("workload"), cell.get("scenario"), cell.get("arm"), why))
            continue
        rolled[(cell["workload"], cell["arm"])].append({
            "quality": quality(cell),
            "cost": float(cell["cost_usd"]),
            "wall": float(cell["wall_s"] or 0),
            "scenario": cell["scenario"],
        })

    by_workload = collections.defaultdict(dict)
    for (workload, arm), rows in rolled.items():
        by_workload[workload][arm] = {
            "arm": arm,
            "cells": len(rows),
            "quality": sum(r["quality"] for r in rows) / len(rows),
            "cost": sum(r["cost"] for r in rows) / len(rows),
            "wall": sum(r["wall"] for r in rows) / len(rows),
        }

    frontier_of = {}
    for workload in sorted(by_workload):
        arms = list(by_workload[workload].values())
        print("\n== %s  (%d arm%s compared)" % (workload, len(arms), "" if len(arms) == 1 else "s"))
        print("   %-10s %6s %9s %9s %9s" % ("arm", "cells", "quality", "cost $", "wall s"))
        for row in sorted(arms, key=lambda r: (-r["quality"], r["cost"])):
            print("   %-10s %6d %8.0f%% %9.4f %9.1f"
                  % (row["arm"], row["cells"], 100 * row["quality"], row["cost"], row["wall"]))
        frontier = [a["arm"] for a in arms if not any(dominates(b, a) for b in arms if b is not a)]
        frontier_of[workload] = set(frontier)
        print("   not dominated: %s" % (", ".join(sorted(frontier)) or "nothing"))
        if len(arms) < 2:
            print("   (one arm is not a comparison — this line says nothing about anybody)")

    if excluded:
        print("\n== excluded from every figure above")
        for workload, scenario, arm, why in sorted(excluded, key=lambda e: (e[0] or "", e[1] or "", e[2] or "")):
            print("   %-14s %-26s %-9s %s" % (workload, scenario, arm, why))

    # The one sentence this tool is allowed to say about the whole run.
    if len(frontier_of) > 1:
        everywhere = set.intersection(*frontier_of.values()) if frontier_of else set()
        print("\n== across workloads")
        if everywhere:
            print("   on the frontier in every workload measured: %s" % ", ".join(sorted(everywhere)))
            print("   this is a statement about %d workload(s) at one effort rung and one model,"
                  % len(frontier_of))
            print("   at the sample sizes in the tables above. It is not a general claim.")
        else:
            print("   no arm is on the frontier in every workload — which is the normal result,")
            print("   and the reason this suite reports per workload rather than in one number.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
