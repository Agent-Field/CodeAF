#!/usr/bin/env python3
"""Cost for the pi and opencode arms, read from their own session stores.

Neither harness prints a bill, but both keep one: pi writes per-message usage
(with its own cost figure) into ~/.pi/agent/sessions/<cwd-keyed dir>/*.jsonl,
and opencode keeps assistant messages with token counts in its sqlite store
(~/.local/share/opencode/opencode.db, session.directory = the cwd). A cell's
sessions are found BY THE CELL'S OWN WORK DIRECTORY, which every cell already
puts in its path, so attribution never leans on timestamps.

Two figures are recorded. `tokens_list_usd` is tokens × OpenRouter list price
for the pinned model — the same arithmetic for every harness, which is the
comparable column. `native_usd` is what the harness itself believed it spent,
kept for the record only (pi's registry prices lag the list).

Usage: competitor_cost.py <results-dir> [cell ...]   (default: every pi-*/opencode-* cell)
Writes cost fields into each cell's meta.json and rewrites results.csv rows.
"""
import csv, glob, json, os, sqlite3, sys, urllib.request

PRICE_CACHE = os.path.join(os.path.dirname(os.path.abspath(__file__)), ".openrouter-prices.json")

def list_prices(model):
    # Cached once per run so a re-collect does not re-fetch on every cell.
    if os.path.exists(PRICE_CACHE):
        cache = json.load(open(PRICE_CACHE))
        if model in cache: return cache[model]
    else:
        cache = {}
    data = json.load(urllib.request.urlopen("https://openrouter.ai/api/v1/models", timeout=30))
    for m in data["data"]:
        p = m["pricing"]
        cache[m["id"]] = {"prompt": float(p.get("prompt") or 0), "completion": float(p.get("completion") or 0),
                          "cache_read": float(p.get("input_cache_read") or p.get("prompt") or 0)}
    json.dump(cache, open(PRICE_CACHE, "w"))
    return cache[model]

def window(cell_dir):
    start = os.path.getmtime(os.path.join(cell_dir, "prompt.txt")) - 120
    end = os.path.getmtime(os.path.join(cell_dir, "pytest-after.log")) + 120
    return start, end

def pi_usage(cell_work, win):
    key = "--" + cell_work.lstrip("/").replace("/", "-") + "--"
    d = os.path.expanduser("~/.pi/agent/sessions/" + key)
    tok = {"input": 0, "output": 0, "cache_read": 0}; native = 0.0; n = 0
    for f in glob.glob(d + "/*.jsonl"):
        for l in open(f, errors="ignore"):
            try: rec = json.loads(l); m = rec.get("message") or {}
            except Exception: continue
            u = m.get("usage")
            if not u: continue
            ts = rec.get("timestamp") or m.get("timestamp")
            if isinstance(ts, str):
                import datetime; ts = datetime.datetime.fromisoformat(ts.replace("Z", "+00:00")).timestamp()
            elif isinstance(ts, (int, float)) and ts > 1e11: ts = ts / 1000
            if ts is not None and not (win[0] <= ts <= win[1]): continue
            n += 1
            tok["input"] += u.get("input", 0); tok["output"] += u.get("output", 0) + u.get("reasoning", 0)
            tok["cache_read"] += u.get("cacheRead", 0)
            native += (u.get("cost") or {}).get("total", 0)
    return tok, native, n

def opencode_usage(cell_work, win):
    db = os.path.expanduser("~/.local/share/opencode/opencode.db")
    c = sqlite3.connect("file:" + db + "?mode=ro", uri=True)
    ids = [r[0] for r in c.execute("select id from session where directory=?", (cell_work,))]
    # Subagent sessions hang off the main one by parent_id; they spent too.
    frontier = list(ids)
    while frontier:
        kids = [r[0] for r in c.execute(
            "select id from session where parent_id in (%s)" % ",".join("?" * len(frontier)), frontier)]
        kids = [k for k in kids if k not in ids]; ids += kids; frontier = kids
    tok = {"input": 0, "output": 0, "cache_read": 0}; native = 0.0; n = 0
    for (data, created) in c.execute("select data, time_created from message where session_id in (%s)" % ",".join("?" * len(ids)), ids) if ids else []:
        if not (win[0] <= created / 1000 <= win[1]): continue
        try: m = json.loads(data)
        except Exception: continue
        t = m.get("tokens")
        if not t: continue
        n += 1
        tok["input"] += t.get("input", 0); tok["output"] += t.get("output", 0) + t.get("reasoning", 0)
        cache = t.get("cache") or {}
        tok["cache_read"] += cache.get("read", 0) if isinstance(cache, dict) else 0
        native += float(m.get("cost") or 0)
    return tok, native, n

def main():
    res = sys.argv[1]
    cells = sys.argv[2:] or [os.path.basename(p) for p in sorted(glob.glob(res + "/pi-*") + glob.glob(res + "/opencode-*"))]
    rows = list(csv.DictReader(open(res + "/results.csv"))); fields = list(rows[0].keys())
    for extra in ("tokens_in", "tokens_out", "tokens_cache", "native_usd"):
        if extra not in fields: fields.append(extra)
    for cell in cells:
        mp = os.path.join(res, cell, "meta.json")
        if not os.path.exists(mp): continue
        meta = json.load(open(mp))
        work = [os.path.abspath(p) for p in glob.glob(os.path.join(res, cell, "work", "*"))]
        if not work: print(cell, "no work dir"); continue
        harness = meta["harness"]
        tok, native, n = (pi_usage if harness == "pi" else opencode_usage)(work[0], window(os.path.join(res, cell)))
        if n == 0: print(cell, "no usage records found"); continue
        p = list_prices(meta["model"])
        usd = tok["input"] * p["prompt"] + tok["output"] * p["completion"] + tok["cache_read"] * p["cache_read"]
        meta.update({"cost_usd": f"{usd:.6f}", "cost_source": "tokens×list-price",
                     "tokens_in": tok["input"], "tokens_out": tok["output"], "tokens_cache": tok["cache_read"],
                     "native_usd": f"{native:.6f}", "usage_records": n})
        json.dump(meta, open(mp, "w"), indent=2)
        for r in rows:
            if r["harness"] == harness and r["task"] == meta["task"] and r["seed"] == meta["seed"]:
                r.update({k: str(meta[k]) for k in ("cost_usd", "cost_source", "tokens_in", "tokens_out", "tokens_cache", "native_usd")})
        print(f"{cell:20} ${usd:.4f} (native ${native:.4f}, {n} calls, in={tok['input']} out={tok['output']} cache={tok['cache_read']})")
    with open(res + "/results.csv", "w", newline="") as fh:
        w = csv.DictWriter(fh, fieldnames=fields); w.writeheader()
        for r in rows: w.writerow({k: r.get(k, "") for k in fields})

if __name__ == "__main__": main()
