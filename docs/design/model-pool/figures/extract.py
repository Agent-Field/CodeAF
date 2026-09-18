#!/usr/bin/env python3
"""Turn the private bench ledger into the three text-free CSV files the figure
script reads.

The ledger rows carry the brief and the reviewer's prose; NONE of that is
written here. What comes out is numbers, model identifiers and coarse category
words -- the same discipline the pool's own rows obey (Section 4 of the paper).
Run it once, from anywhere:

    python3 extract.py --runs ~/codeaf-hero/.codeaf-runs

It rewrites data/runs.csv, data/seatcost.csv and data/cells.csv beside itself.
The CSVs are committed; the ledger is not, so the figures rebuild on a machine
that has never seen it.
"""

import argparse
import csv
import json
import os

HERE = os.path.dirname(os.path.abspath(__file__))
DATA = os.path.join(HERE, "data")

# Fields that may carry task text, a file name or a reviewer's sentence. They
# are named here rather than filtered by a rule so that a field added upstream
# is excluded until somebody looks at it.
NEVER = {"task", "notes", "claim", "contam_note", "diff", "tier_note", "pass_type"}


def load(path):
    with open(path) as fh:
        return [json.loads(line) for line in fh if line.strip()]


def num(value, default=None):
    try:
        return float(value)
    except (TypeError, ValueError):
        return default


def write(name, header, rows):
    path = os.path.join(DATA, name)
    with open(path, "w", newline="") as fh:
        out = csv.writer(fh)
        out.writerow(header)
        out.writerows(rows)
    print("%-16s %4d rows" % (name, len(rows)))


def crew_word(raw):
    """The crew word with its model tail folded away.

    'task+claude-fable-5.1' and 'task+kimi-k3' are the same arm of the design --
    the task door with a named checker -- and the checker is already its own
    column, so the word keeps only the door and the family.
    """
    word = (raw or "").strip()
    if word.startswith("task+learn"):
        return "task+learn"
    if word.startswith("task+"):
        return "task+named"
    if word.startswith("doe-"):
        return "doe"
    if word in ("frugal", "balanced", "max", "do", "task door", "learn-pick"):
        return word
    return "other"


def runs(src):
    rows = []
    for i, r in enumerate(load(os.path.join(src, "ledger.jsonl"))):
        if r.get("rating") is None or r.get("major") is None:
            continue
        rows.append([
            i,
            crew_word(r.get("crew")),
            r.get("worker", ""),
            r.get("planner", ""),
            "%.4f" % num(r.get("cost_usd"), 0.0),
            "" if r.get("checker_usd") is None else "%.4f" % num(r["checker_usd"], 0.0),
            int(num(r.get("calls"), 0)),
            int(num(r.get("wall_s"), 0)),
            "%.0f" % num(r.get("rating"), 0),
            int(num(r.get("major"), 0)),
            int(num(r.get("minor"), 0)),
            int(num(r.get("rules"), 0)),
            "" if r.get("exit") is None else int(num(r["exit"], 0)),
            (r.get("stop") or "").split(":")[0],
        ])
    write("runs.csv",
          ["i", "crew", "worker", "planner", "cost_usd", "checker_usd", "calls",
           "wall_s", "rating", "major", "minor", "rules", "exit", "stop"],
          rows)


def seatcost(src):
    """The designed-experiment ledger: the only rows with a per-seat bill."""
    rows = []
    for i, r in enumerate(load(os.path.join(src, "crew", "doe-results.jsonl"))):
        if r.get("usd_check") is None or not num(r.get("usd")):
            continue
        rows.append([
            i,
            r.get("door", ""),
            r.get("work", ""),
            r.get("plan", ""),
            r.get("check", ""),
            "%.4f" % num(r["usd"], 0.0),
            "%.4f" % num(r.get("usd_work"), 0.0),
            "%.4f" % num(r.get("usd_plan"), 0.0),
            "%.4f" % num(r.get("usd_check"), 0.0),
            "%.4f" % num(r.get("usd_other"), 0.0),
            "%.2f" % num(r.get("wall_min"), 0.0),
            "%.0f" % num(r.get("rating"), 0),
            int(num(r.get("major"), 0)),
            int(num(r.get("minor"), 0)),
            "" if r.get("tokens_in") is None else int(num(r["tokens_in"], 0)),
            "" if r.get("tokens_out") is None else int(num(r["tokens_out"], 0)),
            "" if r.get("calls") is None else int(num(r["calls"], 0)),
        ])
    write("seatcost.csv",
          ["i", "door", "worker", "planner", "checker", "usd", "usd_worker",
           "usd_planner", "usd_checker", "usd_other", "wall_min", "rating",
           "major", "minor", "tokens_in", "tokens_out", "calls"],
          rows)


def cells(src):
    """cell.py's lease/done pairs, joined on the lease id."""
    raw = load(os.path.join(src, "cells.jsonl"))
    lease = {r["id"]: r for r in raw if r.get("kind") == "lease"}
    rows = []
    for d in raw:
        if d.get("kind") != "done" or d["id"] not in lease:
            continue
        l = lease[d["id"]]
        rows.append([
            d["id"],
            l.get("size", ""),
            l.get("type", ""),
            l.get("worker", ""),
            l.get("checker", ""),
            l.get("thinker", ""),
            "%.4f" % num(d.get("usd"), 0.0),
            "%.2f" % num(d.get("minutes"), 0.0),
            "%.0f" % num(d.get("rating"), 0),
            int(num(d.get("major"), 0)),
            int(num(d.get("minor"), 0)),
            1 if d.get("nowork") else 0,
        ])
    write("cells.csv",
          ["id", "size", "type", "worker", "checker", "thinker", "usd",
           "minutes", "rating", "major", "minor", "nowork"],
          rows)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--runs", default=os.path.expanduser("~/codeaf-hero/.codeaf-runs"),
                    help="the directory holding ledger.jsonl, cells.jsonl and crew/")
    args = ap.parse_args()
    os.makedirs(DATA, exist_ok=True)
    runs(args.runs)
    seatcost(args.runs)
    cells(args.runs)
    print("no field in %s was written" % ", ".join(sorted(NEVER)))


if __name__ == "__main__":
    main()
