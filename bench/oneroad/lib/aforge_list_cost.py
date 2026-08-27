#!/usr/bin/env python3
"""List-price cost for aforge cells, from the session's own usage records.

The journal's cost_usd is OpenRouter's REPORTED bill, which depends on the
endpoint the request was routed to. To compare harnesses on one price table,
this reprices every aforge cell's tokens at the model's list price — the same
arithmetic competitor_cost.py applies to pi and opencode. Both figures stay in
the row: `cost_usd` (what was actually billed) and `cost_list_usd` (the fair
comparison), so a routing change shows up as the gap between them closing.
"""
import csv, glob, json, os, sys
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from competitor_cost import list_prices

def usage(cell):
    tok = {"in": 0, "out": 0, "cache": 0}; n = 0
    for f in glob.glob(os.path.join(cell, "profile", "v3", "**", "transcript.jsonl"), recursive=True):
        for l in open(f, errors="ignore"):
            if "costUsd" not in l: continue
            try: d = json.loads(l)
            except Exception: continue
            def walk(o):
                nonlocal n
                if isinstance(o, dict):
                    if "costUsd" in o and "input" in o:
                        n += 1; tok["in"] += o.get("input", 0); tok["out"] += o.get("output", 0); tok["cache"] += o.get("cacheRead", 0)
                    for v in o.values(): walk(v)
                elif isinstance(o, list):
                    for v in o: walk(v)
            walk(d)
    return tok, n

def main():
    res = sys.argv[1]
    rows = list(csv.DictReader(open(res + "/results.csv"))); fields = list(rows[0].keys())
    if "cost_list_usd" not in fields: fields.append("cost_list_usd")
    for r in rows:
        if not r["harness"].startswith("aforge"): continue
        cell = os.path.join(res, f'{r["harness"]}-{r["task"]}-{r["seed"]}')
        tok, n = usage(cell)
        if n == 0: continue
        p = list_prices(r["model"] or "deepseek/deepseek-v4-flash")
        # `input` is prompt_tokens in the OpenAI dialect and INCLUDES the cached
        # part; only the remainder is billed at the uncached rate.
        uncached = max(0, tok["in"] - tok["cache"])
        usd = uncached * p["prompt"] + tok["cache"] * p["cache_read"] + tok["out"] * p["completion"]
        r["cost_list_usd"] = f"{usd:.6f}"
        mp = os.path.join(cell, "meta.json")
        if os.path.exists(mp):
            m = json.load(open(mp)); m["cost_list_usd"] = r["cost_list_usd"]; json.dump(m, open(mp, "w"), indent=2)
    with open(res + "/results.csv", "w", newline="") as fh:
        w = csv.DictWriter(fh, fieldnames=fields); w.writeheader()
        for r in rows: w.writerow({k: r.get(k, "") for k in fields})
    print("repriced", sum(1 for r in rows if r.get("cost_list_usd")), "aforge cells")

if __name__ == "__main__": main()
