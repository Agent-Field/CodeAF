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
import re
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


def variant_of(run_dir, meta):
    # The reasoning effort the run ran at — meta.json first, then the run
    # directory's -e<effort> suffix. It groups the per-level average.
    v = meta.get("variant")
    if v:
        return v
    m = re.search(r"-e([^-]+)$", os.path.basename(run_dir))
    return m.group(1) if m else ""


def row(run_dir):
    final = final_row(run_dir)
    meta = read(os.path.join(run_dir, "meta.json"))
    variant = variant_of(run_dir, meta)
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
            "variant": variant,
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
        "variant": variant,
        "note": final.get("notes") or "",
    }


def main():
    dirs = sys.argv[1:]
    if not dirs:
        root = pathlib.Path(RESULTS)
        dirs = sorted(str(p) for p in root.iterdir() if p.is_dir() and not p.name.startswith("."))
    rows = [row(d) for d in dirs]
    print(f"{'DIR':44} {'TASK':26} {'ARM':12} {'MODEL':20} {'EFF':>5} "
          f"{'SCORE':>6} {'PASS':>4} {'FLAG':>7} {'COST$':>7} {'GUARD$':>7} {'OUT TOK':>8} {'WALL':>6} NOTE")
    print("-" * 180)
    passes = flags = costed = 0
    total_cost = 0.0
    for r in rows:
        print(f"{r['dir'][:44]:44} {r['task'][:26]:26} {r['arm'][:12]:12} {r['model'][:20]:20} {str(r['variant'])[:5]:>5} "
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

    # Per-level aggregate, the official protocol's shape: the metric is
    # averaged across a level's trials, and the best-performing level is
    # reported. A run that graded rig is never averaged in.
    levels: dict = {}
    for r in rows:
        levels.setdefault(r["variant"] or "?", []).append(r)
    if rows and (len(levels) > 1 or (rows[0]["variant"] or "?") != "?"):
        print(f"\n{'EFFORT':>8} {'TRIALS':>6} {'GRADED':>6} {'PASS':>5} {'MEAN SCORE':>10} {'MEAN OUT TOK':>12}")
        print("-" * 52)
        best = None
        for eff in sorted(levels):
            rs = levels[eff]
            graded = [r for r in rs if r["score"] not in ("rig", "")]
            scores = [float(r["score"]) for r in graded]
            toks = [float(r["out_tok"]) for r in graded if r["out_tok"] not in ("", None)]
            mean = sum(scores) / len(scores) if scores else None
            if mean is not None and (best is None or mean > best[1]):
                best = (eff, mean)
            print(f"{eff:>8} {len(rs):>6} {len(graded):>6} "
                  f"{sum(r['pass'] == 'yes' for r in graded):>5} "
                  f"{'n/a' if mean is None else f'{mean:.4f}':>10} "
                  f"{f'{sum(toks) / len(toks):.0f}' if toks else 'n/a':>12}")
        if best:
            print(f"best-performing reasoning effort: {best[0]} (mean score {best[1]:.4f})")


if __name__ == "__main__":
    main()