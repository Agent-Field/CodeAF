#!/usr/bin/env python3
"""The scoreboard: one row per run directory, read off each run's final
record.jsonl row. The columns are the ones the rig's protocol fixes: score,
pass, flag, cost per rollout, output tokens, wall time — with the cost read
twice where both readings exist (harness-reported, guard-metered).

A run with no final row prints as `rig` with the stage it stopped at — a
missing grade is never averaged in as a zero and never silently dropped.

    report.py [<results-dir>] [run-dir ...]
"""

import json
import os
import pathlib
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
RESULTS = os.environ.get("RESULTS", os.path.join(HERE, "..", "results"))

MISSING = object()


def read(path, default=MISSING):
    try:
        with open(path) as fh:
            return json.load(fh)
    except Exception:
        return {} if default is MISSING else default


def final_row(run_dir):
    path = pathlib.Path(run_dir) / "record.jsonl"
    if not path.exists():
        return None
    last = None
    for line in path.read_text().split("\n"):
        if line.strip():
            last = json.loads(line)
    return last if last and last.get("event") == "final" else None


def row(run_dir):
    final = final_row(run_dir)
    meta = read(os.path.join(run_dir, "meta.json"))
    if not final:
        return {
            "dir": os.path.basename(run_dir),
            "task": meta.get("task", os.path.basename(run_dir)),
            "arm": meta.get("arm", ""),
            "model": meta.get("model", ""),
            "score": "rig",
            "pass": "",
            "flag": "",
            "cost": meta.get("cost_usd_harness", 0.0) or 0.0,
            "cost_guard": "",
            "out_tok": "",
            "wall": meta.get("agent_seconds", ""),
            "note": f"no final row (stage={meta.get('stage', '?')})",
        }
    cost_guard = ("" if final.get("cost_usd_guard") is None
                  else f"{final['cost_usd_guard']:.3f}")
    return {
        "dir": final["task"] and os.path.basename(run_dir),
        "task": final.get("task", ""),
        "arm": final.get("arm", ""),
        "model": (final.get("model") or "").split("/")[-1],
        "score": "rig" if final.get("score") is None else f"{final['score']:.2f}",
        "pass": "yes" if final.get("pass") else "no",
        "flag": "FLAGGED" if final.get("flagged") else "",
        "cost": final.get("cost_usd_harness") or 0.0,
        "cost_guard": cost_guard,
        "out_tok": final.get("completion_tokens") if final.get("completion_tokens") is not None else "",
        "wall": final.get("agent_seconds", ""),
        "note": final.get("notes") or "",
    }


def main():
    dirs = sys.argv[1:]
    if not dirs:
        root = pathlib.Path(RESULTS)
        dirs = sorted(str(p) for p in root.iterdir() if p.is_dir() and not p.name.startswith(".")
                      and not p.name.endswith("-gold") and not p.name.endswith("-negative")
                      and not p.name.endswith("-seal"))
    rows = [row(d) for d in dirs]
    print(f"{'DIR':44} {'TASK':30} {'ARM':12} {'MODEL':22} "
          f"{'SCORE':>6} {'PASS':>4} {'FLAG':>7} {'COST$':>7} {'GUARD$':>7} {'OUT TOK':>8} {'WALL':>6} NOTE")
    print("-" * 175)
    passes = flags = costed = 0
    total_cost = 0.0
    for r in rows:
        print(f"{r['dir'][:44]:44} {r['task'][:30]:30} {r['arm'][:12]:12} {r['model'][:22]:22} "
              f"{str(r['score']):>6} {r['pass'][:4]:>4} {r['flag'][:7]:>7} {r['cost']:>7.3f} {r['cost_guard']:>7} "
              f"{str(r['out_tok']):>8} {str(r['wall']):>6} {r['note'][:60]}")
        if r["score"] not in ("rig", ""):
            costed += 1
            passes += r["pass"] == "yes"
            flags += r["flag"] == "FLAGGED"
        total_cost += r["cost"] or 0.0
    print()
    print(f"runs {len(rows)}: {passes} pass, {flags} flagged, {costed} graded, "
          f"{len(rows) - costed} rig; total ${total_cost:.3f} (harness-reported)")


if __name__ == "__main__":
    main()