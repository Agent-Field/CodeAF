#!/usr/bin/env python3
"""Summarise an arm's results.jsonl: per-task success, cost, time, failure modes.

Usage: python3 summarize.py results-armA.jsonl [--arm a] [--json]
"""
import argparse
import json
import statistics
import sys


def load(path, arm=None):
    rows = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            r = json.loads(line)
            if arm is None or r["arm"] == arm:
                rows.append(r)
    return rows


def spread(values):
    if not values:
        return {"n": 0}
    values = sorted(values)
    return {
        "n": len(values), "min": values[0], "max": values[-1],
        "median": statistics.median(values),
        "mean": round(statistics.fmean(values), 4),
        "total": round(sum(values), 4),
    }


def summarize(rows):
    out = {"cells": len(rows), "by_task": {}}
    for task in sorted({r["task"] for r in rows}):
        cells = [r for r in rows if r["task"] == task]
        stops = {}
        for r in cells:
            for k, v in r["stop_reasons"].items():
                stops[k] = stops.get(k, 0) + v
        out["by_task"][task] = {
            "n": len(cells),
            "successes": sum(1 for r in cells if r["success"]),
            "success_rate": round(sum(1 for r in cells if r["success"]) / len(cells), 4),
            "score": spread([r["score"] for r in cells]),
            "cost_usd": spread([r["cost_usd"] for r in cells]),
            "wall_seconds": spread([r["wall_seconds"] for r in cells]),
            "turns": spread([r["turns"] for r in cells]),
            "calls": spread([r["calls"] for r in cells]),
            "prompt_tokens": spread([r["prompt_tokens"] for r in cells]),
            "completion_tokens": spread([r["completion_tokens"] for r in cells]),
            "leaves": spread([r["leaves_total"] for r in cells]),
            "leaves_not_done": sum(r["leaves_total"] - r["leaves_done"] for r in cells),
            "stop_reasons": stops,
            "plan_failures": sum(1 for r in cells if r["plan_exit"] != 0),
            "run_nonzero_exit": sum(1 for r in cells if r["run_exit"] not in (0,)),
        }
    out["overall"] = {
        "success_rate": round(sum(1 for r in rows if r["success"]) / max(len(rows), 1), 4),
        "score": spread([r["score"] for r in rows]),
        "cost_usd": spread([r["cost_usd"] for r in rows]),
        "wall_seconds": spread([r["wall_seconds"] for r in rows]),
        "total_spend": round(sum(r["cost_usd"] for r in rows), 4),
    }
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("jsonl")
    ap.add_argument("--arm", default=None)
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args()
    rows = load(args.jsonl, args.arm)
    if not rows:
        print("no rows", file=sys.stderr)
        return
    s = summarize(rows)
    if args.json:
        print(json.dumps(s, indent=2))
        return
    print(f"{'task':<15} {'succ':>7} {'score':>16} {'$ mean':>9} "
          f"{'s median':>9} {'turns med':>10} {'stops'}")
    for task, t in s["by_task"].items():
        print(f"{task:<15} {t['successes']}/{t['n']:<5} "
              f"{t['score']['min']:.2f}-{t['score']['max']:.2f} "
              f"(med {t['score']['median']:.2f}) "
              f"{t['cost_usd']['mean']:>9.4f} "
              f"{t['wall_seconds']['median']:>9.0f} "
              f"{t['turns']['median']:>10.0f}  {t['stop_reasons']}")
    o = s["overall"]
    print(f"\noverall success {o['success_rate']:.3f}  "
          f"median score {o['score']['median']:.3f}  "
          f"total spend ${o['total_spend']:.4f}  "
          f"wall {o['wall_seconds']['total'] / 60:.1f} min")


if __name__ == "__main__":
    main()
