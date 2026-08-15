#!/usr/bin/env python3
"""Arm A against arm B, per task and overall.

Reports the four things the experiment was designed to answer — success, score,
cost, wall clock — plus the two that decide whether any of them mean anything:
the **node count** the planner happened to draw for each cell, and the model
mix the router actually used.

The node count is not decoration. Arm A's own replicates spanned 5 to 26 nodes
for a byte-identical brief at 5.4x the cost and the same score, so a cost
difference between the arms that is smaller than the planner's own spread is
not evidence about routing. It is printed next to every cost so the two cannot
be read apart.

Usage: python3 compare.py results-armA.jsonl results-armB.jsonl [--json]
"""
import argparse
import json
import statistics
from collections import Counter


def load(path):
    rows = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if line:
                rows.append(json.loads(line))
    return rows


def stat(values):
    if not values:
        return {"n": 0}
    return {"n": len(values), "min": min(values), "max": max(values),
            "median": statistics.median(values),
            "mean": round(statistics.fmean(values), 4),
            "total": round(sum(values), 4)}


def models_used(rows):
    out = Counter()
    for r in rows:
        for slug, n in ((r.get("routing") or {}).get("by_model") or {}).items():
            out[slug.split("/")[-1]] += n
    return out


def escalated(rows):
    return sum((r.get("routing") or {}).get("escalated_calls", 0) for r in rows)


def per_task(rows):
    out = {}
    for task in sorted({r["task"] for r in rows}):
        cells = [r for r in rows if r["task"] == task]
        stops = Counter()
        for r in cells:
            stops.update(r["stop_reasons"])
        out[task] = {
            "n": len(cells),
            "successes": sum(1 for r in cells if r["success"]),
            "score": stat([r["score"] for r in cells]),
            "cost": stat([r["cost_usd"] for r in cells]),
            "wall": stat([r["wall_seconds"] for r in cells]),
            "turns": stat([r["turns"] for r in cells]),
            "nodes": [r["nodes_total"] for r in cells],
            "stops": dict(stops),
            "models": dict(models_used(cells)),
            "escalated_calls": escalated(cells),
        }
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("arm_a")
    ap.add_argument("arm_b")
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args()

    a, b = load(args.arm_a), load(args.arm_b)
    A, B = per_task(a), per_task(b)
    tasks = sorted(set(A) | set(B))

    result = {"arm_a": A, "arm_b": B, "overall": {}}
    for name, rows in (("a", a), ("b", b)):
        result["overall"][name] = {
            "cells": len(rows),
            "successes": sum(1 for r in rows if r["success"]),
            "success_rate": round(sum(1 for r in rows if r["success"]) / max(len(rows), 1), 4),
            "score": stat([r["score"] for r in rows]),
            "cost_total": round(sum(r["cost_usd"] for r in rows), 4),
            "wall_total": sum(r["wall_seconds"] for r in rows),
            "models": dict(models_used(rows)),
            "escalated_calls": escalated(rows),
        }

    if args.json:
        print(json.dumps(result, indent=2))
        return

    print(f"{'task':<15} {'A succ':>7} {'B succ':>7} {'A score':>8} {'B score':>8} "
          f"{'A $':>8} {'B $':>8} {'A s':>7} {'B s':>7} {'A nodes':>12} {'B nodes':>12}")
    for task in tasks:
        x, y = A.get(task), B.get(task)
        if not (x and y):
            continue
        print(f"{task:<15} {x['successes']}/{x['n']:<5} {y['successes']}/{y['n']:<5} "
              f"{x['score']['median']:>8.3f} {y['score']['median']:>8.3f} "
              f"{x['cost']['mean']:>8.4f} {y['cost']['mean']:>8.4f} "
              f"{x['wall']['median']:>7.0f} {y['wall']['median']:>7.0f} "
              f"{str(x['nodes']):>12} {str(y['nodes']):>12}")

    oa, ob = result["overall"]["a"], result["overall"]["b"]
    print(f"\noverall   A {oa['successes']}/{oa['cells']} = {oa['success_rate']:.3f}"
          f"   B {ob['successes']}/{ob['cells']} = {ob['success_rate']:.3f}")
    print(f"score     A median {oa['score']['median']:.3f}   B median {ob['score']['median']:.3f}")
    print(f"spend     A ${oa['cost_total']:.4f}   B ${ob['cost_total']:.4f}"
          f"   ({(ob['cost_total'] / oa['cost_total'] - 1) * 100:+.0f}%)")
    print(f"wall      A {oa['wall_total'] / 60:.1f} min   B {ob['wall_total'] / 60:.1f} min")
    print(f"models    B: {ob['models']}")
    print(f"escalated B: {ob['escalated_calls']} calls")


if __name__ == "__main__":
    main()
