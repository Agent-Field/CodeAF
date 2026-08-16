#!/usr/bin/env python3
"""Split the shared-ledger arm into its passes, and say what is trustworthy.

The arm was interrupted twice by background-process kills and resumed twice,
and the second resume was launched while the first was still alive. Two writers
therefore shared one ledger and one results file for the last two passes. What
that does and does not damage is worth being precise about, because the two
halves have very different reliability:

  * **the ledger is fine.** It is locked (internal/router/ledger.go takes an
    exclusive-create lock per update), and the event log is append-only. No
    observation was lost or double-counted.
  * **the scores are fine.** Every cell ran its own plan, its own workspace and
    its own grader; concurrency changes provider contention, not correctness.
  * **per-cell event attribution is not fine** for the overlapping passes. The
    runner slices the append-only event log by line position, and with two
    writers a cell's slice can contain rows the other process wrote. Model
    counts for passes 3 and 4 are therefore approximate.

So this writes two files. `results-armB.jsonl` is the n=3 experiment — the
first occurrence of each (task, replicate), which is the pass that ran in the
intended sequence. `results-armB-pass4.jsonl` is the accidental fourth pass,
kept because it independently reproduced the collapse and a second observation
of a surprising result is worth more than tidiness.

Usage: python3 dedupe-armB.py results-armb.jsonl
"""
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))


def main():
    source = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "results-armb.jsonl")
    rows = []
    with open(source) as f:
        for line in f:
            line = line.strip()
            if line:
                rows.append(json.loads(line))

    first, extra, seen = [], [], set()
    for row in rows:
        key = (row["task"], row["rep"])
        if key in seen:
            row["pass"] = 4
            row["note"] = ("accidental fourth pass: a second resume was launched "
                           "while the first was still running, so this cell shared "
                           "the ledger and the event log with another writer")
            extra.append(row)
        else:
            seen.add(key)
            row["pass"] = row["rep"]
            first.append(row)

    first.sort(key=lambda r: (r["rep"], r["task"]))
    out = os.path.join(HERE, "results-armB.jsonl")
    with open(out, "w") as f:
        for row in first:
            f.write(json.dumps(row) + "\n")
    if extra:
        extra_path = os.path.join(HERE, "results-armB-pass4.jsonl")
        with open(extra_path, "w") as f:
            for row in extra:
                f.write(json.dumps(row) + "\n")
        print(f"{len(extra)} rows -> {os.path.basename(extra_path)}")
    print(f"{len(first)} rows -> {os.path.basename(out)}")
    for row in first:
        print(f"  rep{row['rep']} {row['task']:<14} {row['score']:.3f}")


if __name__ == "__main__":
    main()
