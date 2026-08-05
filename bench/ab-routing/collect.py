#!/usr/bin/env python3
"""Turn one finished cell into one JSONL row.

Everything measurable about a run is already written down by aforge itself: the
completed graph (`done.json`) carries per-node turns, tokens, stop reason and
state, and `graph.usage` carries the run's calls, prompt/completion/cached
tokens and cost, summed by the scheduler from the provider's own per-response
accounting. That is the number bench/README.md insists on -- an account-level
credit delta on a shared key measures other people's traffic too.

The grade comes from the task's own grader, run here rather than in the shell so
a grader that crashes produces a recorded row with the traceback instead of a
missing cell.
"""
import argparse
import json
import os
import subprocess
import sys


def load_graph(path):
    try:
        with open(path) as f:
            return json.load(f)
    except Exception:
        return None


def grade(tasks_dir, task, workspace):
    grader = os.path.join(tasks_dir, task, "grade.py")
    try:
        p = subprocess.run([sys.executable, grader, workspace, "--json"],
                           capture_output=True, text=True, timeout=900)
    except subprocess.TimeoutExpired:
        return {"score": 0.0, "success": False, "_grader": "timed out"}
    lines = [l for l in p.stdout.splitlines() if l.startswith("{")]
    if not lines:
        return {"score": 0.0, "success": False,
                "_grader": (p.stdout + p.stderr)[-600:]}
    return json.loads(lines[-1])


def ledger_snapshot(cell):
    """Whatever the harness wrote to the profile directory, verbatim.

    The router does not exist yet, so this cannot know the shape of a routing
    event. It records every file under the ledger so the arm-B learning check
    can diff run 1 against run 3 whatever format the router settles on, and so
    arm A's rows carry evidence that each replicate really did start cold.
    """
    root = os.path.join(cell, "ledger-after")
    out = {"files": {}, "bytes": 0}
    if not os.path.isdir(root):
        return out
    for dirpath, _dirs, files in os.walk(root):
        for name in sorted(files):
            path = os.path.join(dirpath, name)
            relative = os.path.relpath(path, root)
            try:
                size = os.path.getsize(path)
                with open(path, encoding="utf-8", errors="replace") as f:
                    body = f.read()
            except OSError:
                continue
            out["bytes"] += size
            entry = {"bytes": size}
            try:
                entry["json"] = json.loads(body)
            except Exception:
                entry["lines"] = body.count("\n") + 1 if body else 0
                entry["text"] = body[:4000]
            out["files"][relative] = entry
    return out


def main():
    ap = argparse.ArgumentParser()
    for name in ("arm", "task", "rep", "cell", "workspace", "tasks-dir",
                 "plan-seconds", "run-seconds", "plan-exit", "run-exit",
                 "ledger-mode"):
        ap.add_argument("--" + name, required=True)
    args = ap.parse_args()

    graph = load_graph(os.path.join(args.cell, "done.json")) or \
        load_graph(os.path.join(args.cell, "graph.json"))

    row = {
        "arm": args.arm, "task": args.task, "rep": int(args.rep),
        "ledger_mode": args.ledger_mode,
        "plan_seconds": int(args.plan_seconds),
        "run_seconds": int(args.run_seconds),
        "wall_seconds": int(args.plan_seconds) + int(args.run_seconds),
        "plan_exit": int(args.plan_exit), "run_exit": int(args.run_exit),
        "cell": os.path.relpath(args.cell, os.path.dirname(os.path.abspath(__file__))),
    }

    usage = (graph or {}).get("usage") or {}
    row["cost_usd"] = float(usage.get("cost") or 0.0)
    row["calls"] = int(usage.get("calls") or 0)
    row["prompt_tokens"] = int(usage.get("prompt_tokens") or 0)
    row["completion_tokens"] = int(usage.get("completion_tokens") or 0)
    row["cached_tokens"] = int(usage.get("cached_tokens") or 0)

    nodes = (graph or {}).get("nodes") or []
    leaves = [n for n in nodes if n.get("kind") in (None, "", "work", "synthesis")
              and not any(m.get("parent") == n.get("id") for m in nodes)]
    row["nodes_total"] = len(nodes)
    row["leaves_total"] = len(leaves)
    row["leaves_done"] = sum(1 for n in leaves if n.get("state") == "done")
    row["leaves_failed"] = sum(1 for n in leaves if n.get("state") == "failed")
    row["leaves_blocked"] = sum(1 for n in leaves if n.get("state") == "blocked")
    row["leaves_never_started"] = sum(1 for n in leaves if n.get("state") == "pending")
    row["turns"] = sum(int(n.get("turns") or 0) for n in nodes)
    row["node_tokens"] = sum(int(n.get("tokens") or 0) for n in nodes)

    stops = {}
    for n in nodes:
        stop = n.get("stop")
        if stop:
            stops[stop] = stops.get(stop, 0) + 1
    row["stop_reasons"] = stops
    row["per_leaf"] = [
        {"id": n.get("id"), "title": (n.get("title") or "")[:48],
         "state": n.get("state"), "stop": n.get("stop"),
         "turns": n.get("turns"), "tokens": n.get("tokens"),
         "failure": (n.get("failure") or "")[:200]}
        for n in leaves]

    graded = grade(args.tasks_dir, args.task, args.workspace)
    row["score"] = float(graded.get("score") or 0.0)
    row["success"] = bool(graded.get("success"))
    row["grade"] = graded

    row["ledger"] = ledger_snapshot(args.cell)
    print(json.dumps(row))


if __name__ == "__main__":
    main()
