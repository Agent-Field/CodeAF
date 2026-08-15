#!/usr/bin/env python3
"""Merge the two rounds into results.jsonl.

results.jsonl is the complete raw record: every API call from both rounds,
each tagged with `round` and `final`. `final` marks the 218 cells that make up
the analysed outcome matrix -- round 1 for the 24 tasks that were never
touched, round 2 for the 12 that were hardened. Nothing is deleted, so the
calibration round stays auditable.
"""
import json
import os

import tasks as T

HERE = os.path.dirname(os.path.abspath(__file__))
HARD = set(T.HARDENED_IDS)


def load(name, rnd):
    path = os.path.join(HERE, name)
    out = []
    with open(path) as f:
        for line in f:
            r = json.loads(line)
            r["round"] = rnd
            r["final"] = (rnd == 2) if r["task"] in HARD else (rnd == 1)
            out.append(r)
    return out


def main():
    recs = load("results_round1.jsonl", 1) + load("results_round2_hardened.jsonl", 2)
    recs.sort(key=lambda r: (r["task"], r["model"], r["rep"], r["round"]))
    with open(os.path.join(HERE, "results.jsonl"), "w") as f:
        for r in recs:
            f.write(json.dumps(r) + "\n")
    final = [r for r in recs if r["final"]]
    keys = {(r["model"], r["task"], r["rep"]) for r in final}
    print(f"total calls:  {len(recs)}")
    print(f"final matrix: {len(final)} cells, {len(keys)} unique")
    assert len(final) == len(keys) == 218, "final matrix is not the design"
    print(f"spend:        ${sum(r['cost_usd'] for r in recs):.4f} "
          f"(final matrix ${sum(r['cost_usd'] for r in final):.4f})")
    print("wrote results.jsonl")


if __name__ == "__main__":
    main()
