#!/usr/bin/env python3
"""score.py — score every scriptbench cell and write results.csv.

Usage: score.py <results-dir>

For each cell: the conversation's last reply (written to reply.txt beside it)
scored by check.py, the script counts from lib/script_use.py, and the cell's
whole bill read from its own usage ledger (v3/usage.jsonl, every row once),
which counts the conversation and any task alike. Then one line per arm and
task: mean score, mean dollars, mean input and output tokens, scripts written.
"""
import csv, glob, json, os, subprocess, sys
from collections import defaultdict

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT_USE = os.path.join(HERE, "..", "oneroad", "lib", "script_use.py")


def text_of(content):
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return "".join(p.get("text", "") for p in content if isinstance(p, dict))
    return ""


def last_reply(cell):
    found = glob.glob(os.path.join(cell, "profile", "v3", "projects", "*", "*", ".cell", "transcript.jsonl"))
    reply = ""
    for path in found:
        for line in open(path, encoding="utf-8"):
            try:
                row = json.loads(line)
            except json.JSONDecodeError:
                continue
            if row.get("type") == "message" and row.get("role") == "assistant":
                text = text_of(row.get("content"))
                if text.strip():
                    reply = text
    return reply


def bill(cell):
    rows = {}
    for path in glob.glob(os.path.join(cell, "profile", "v3", "usage.jsonl")):
        for line in open(path, encoding="utf-8"):
            try:
                row = json.loads(line)
            except json.JSONDecodeError:
                continue
            rows[row.get("id") or len(rows)] = row
    out = {"usd": 0.0, "in": 0, "out": 0, "cache": 0, "model_calls": 0}
    for row in rows.values():
        out["usd"] += float(row.get("usd") or 0)
        out["in"] += int(row.get("in") or 0)
        out["out"] += int(row.get("out") or 0)
        out["cache"] += int(row.get("cache_read") or 0)
        out["model_calls"] += int(row.get("calls") or 1)
    out["usd"] = round(out["usd"], 6)
    return out


def main(results):
    rows = []
    for meta_path in sorted(glob.glob(os.path.join(results, "*", "meta.json"))):
        cell = os.path.dirname(meta_path)
        meta = json.load(open(meta_path))
        task = meta.get("task")
        reply = last_reply(cell)
        with open(os.path.join(cell, "reply.txt"), "w", encoding="utf-8") as fh:
            fh.write(reply)
        repos = glob.glob(os.path.join(cell, "work", "*"))
        if not repos:
            # A cell that never cloned has nothing to check a reply against.
            print(f"skipping {os.path.basename(cell)}: no repository under work/", file=sys.stderr)
            continue
        repo = repos[0]
        checked = json.loads(subprocess.run(
            [sys.executable, os.path.join(HERE, "check.py"), "score", task, repo, os.path.join(cell, "reply.txt")],
            capture_output=True, text=True, check=True).stdout)
        use = json.loads(subprocess.run([sys.executable, SCRIPT_USE, cell], capture_output=True, text=True, check=True).stdout)
        cost = bill(cell)
        json.dump({"check": checked, "use": use, "bill": cost}, open(os.path.join(cell, "score.json"), "w"), indent=1)
        rows.append({
            "arm": meta.get("harness"), "task": task, "seed": meta.get("seed"), "model": meta.get("model"),
            "outcome": meta.get("outcome"), "wall_s": meta.get("wall_s"), "score": checked.get("score"),
            "usd": cost["usd"], "tokens_in": cost["in"], "tokens_out": cost["out"], "tokens_cache": cost["cache"],
            "model_calls": cost["model_calls"], "road": meta.get("road"),
            "chat_steps": use.get("chat_steps"), "chat_scripts": use.get("chat_script_steps"),
            "chat_script_calls": use.get("chat_script_calls"),
            "worker_steps": use.get("steps"), "worker_scripts": use.get("script_steps"),
        })
    if not rows:
        print("no cells")
        return
    with open(os.path.join(results, "results.csv"), "w", newline="") as fh:
        writer = csv.DictWriter(fh, fieldnames=list(rows[0]))
        writer.writeheader()
        writer.writerows(rows)
    groups = defaultdict(list)
    for row in rows:
        groups[(row["task"], row["arm"])].append(row)
    print(f"{'task':12} {'arm':24} {'n':>2} {'score':>6} {'usd':>7} {'in':>9} {'out':>7} {'calls':>5} {'steps':>5} {'scripts':>7} {'wall':>5}")
    for (task, arm), group in sorted(groups.items()):
        n = len(group)
        mean = lambda k: sum((r[k] or 0) for r in group) / n
        print(f"{task:12} {arm:24} {n:>2} {mean('score'):>6.2f} {mean('usd'):>7.3f} {mean('tokens_in'):>9.0f} "
              f"{mean('tokens_out'):>7.0f} {mean('model_calls'):>5.0f} {mean('chat_steps'):>5.0f} "
              f"{sum(r['chat_scripts'] or 0 for r in group):>7} {mean('wall_s'):>5.0f}")


if __name__ == "__main__":
    main(sys.argv[1])
