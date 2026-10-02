#!/usr/bin/env python3
"""summarize.py <out dir>: rows.jsonl -> summary.json and summary.md (median [min-max] of the reps).

Dollars use the hosted price table of docs/STAGE-1H-DECISION.md (marginal, beyond every free tier), rule (a):
  Worker request $0.30/M (every relay request), Worker CPU 3 ms assumed per request at $0.02/M ms,
  Durable Object request $0.15/M (every directory request), R2 Class A $4.50/M (a frame put),
  R2 Class B $0.36/M (an object get; one B per get is a lower bound), R2 storage $0.015/GB-month.
Operation counts come from the client's own wire meter, so they are what the Worker would be asked, not
what the docker relay did.
"""
import json, statistics, sys
from collections import defaultdict
from pathlib import Path

P = dict(worker_req=0.30e-6, cpu_ms=0.02e-6, cpu_per_req_ms=3.0, do_req=0.15e-6, r2_a=4.50e-6, r2_b=0.36e-6, gb_month=0.015)
ORDER = {"S": 0, "M": 1, "L": 2, "XL": 3}
KIND_ORDER = {"one": 0, "fifty": 1, "big": 2}


def device_name(rows, env, who):
    """The device a latency table row is about: A is this box; the other is whichever second device the env ran (A2 on one box)."""
    if who == "a":
        return "A"
    others = {r["machine"] for r in rows if r["env"] == env} - {"A"}
    return min(others) if others else "A2"


def counts(wire):
    g = lambda k: wire.get(k, {})
    dir_n = sum(v["n"] for k, v in wire.items() if k.startswith("dir_"))
    put, get, has = g("store_put"), g("store_get"), g("store_has")
    up = sum(v["up"] for v in wire.values())
    down = sum(v["down"] for v in wire.values())
    return {"requests": dir_n + sum(v["n"] for k, v in wire.items() if k.startswith("store_")), "dir_requests": dir_n,
            "puts": put.get("n", 0), "gets": get.get("n", 0), "has": has.get("n", 0), "bytes_up": up, "bytes_down": down,
            "put_bytes": put.get("up", 0), "get_bytes": get.get("down", 0)}


def dollars(c, stored_bytes=0):
    """dollars of one operation from its counts; storage is one month of the bytes it adds."""
    a, b = c["puts"], c["gets"] + c["has"]
    ops = c["requests"] * P["worker_req"] + c["requests"] * P["cpu_per_req_ms"] * P["cpu_ms"] + c["dir_requests"] * P["do_req"] + a * P["r2_a"] + b * P["r2_b"]
    return ops + stored_bytes / 1e9 * P["gb_month"]


def med(xs):
    xs = sorted(xs)
    return statistics.median(xs) if xs else None


def cell(xs, f=lambda v: f"{v:,.0f}"):
    xs = [x for x in xs if x is not None]
    if not xs:
        return "-"
    lo, hi = min(xs), max(xs)
    return f"{f(med(xs))} [{f(lo)}-{f(hi)}]" if len(xs) > 1 else f(xs[0])


def sec(ms):
    return f"{ms / 1000:,.1f}" if ms >= 1000 else f"{ms / 1000:,.2f}"


def usd(v):
    return f"${v:.5f}" if v < 0.01 else f"${v:.3f}"


def mb(b):
    return f"{b / 1e6:,.1f}" if b >= 1e5 else f"{b / 1e3:,.0f} kB"


def load(out):
    return [json.loads(l) for l in open(Path(out) / "rows.jsonl") if l.strip()]


def flat(r):
    """one comparable record per row: time, counts, phases, dollars."""
    if "take" in r:
        t = r["take"]
        c = counts(t["wire"])
        d = {"ms": t["total_ms"], "phases": t["phases_ms"], "want_rounds": len(t["engine"].get("want", {}).get("want_sizes", [])), **c}
        d["usd"] = dollars(c)
        return d
    t = r["turn"]
    c = counts(t["wire"])
    c["puts"] = c["puts"] or 0
    d = {"ms": t["durable_ms"], "seal_ms": t["seal_ms"], "push_ms": t["push_ms"], "changed": t["changed_files"], "objects": t["objects"],
         "bytes_up_payload": t["bytes_up"], "export_ms": t["engine"].get("export", {}).get("ms", 0),
         "put_ms": t["wire"].get("store_put", {}).get("ms", 0), **c}
    d["usd"] = dollars(c, stored_bytes=t["bytes_up"])
    return d


def group(rows):
    g = defaultdict(list)
    for r in rows:
        if r["scenario"] == "error":
            continue
        key = (r["env"], r["size"], r["scenario"], r["kind"])
        g[key].append(dict(flat(r), repo_bytes=r["repo_bytes"], machine=r["machine"], sha=r["sha"]))
    return g


def summary(rows):
    out = {}
    for (env, size, scen, kind), xs in group(rows).items():
        out[f"{env}|{size}|{scen}|{kind}"] = {
            "env": env, "size": size, "scenario": scen, "kind": kind, "n": len(xs), "repo_bytes": xs[0]["repo_bytes"], "machine": xs[0]["machine"],
            "median": {k: med([x[k] for x in xs]) for k in xs[0] if isinstance(xs[0][k], (int, float))},
            "min_ms": min(x["ms"] for x in xs), "max_ms": max(x["ms"] for x in xs),
            "phases_median_ms": {p: med([x["phases"][p] for x in xs]) for p in xs[0].get("phases", {})} if "phases" in xs[0] else None}
    return out


PROFILE = dict(sessions=8, publishes_per_session=60, heartbeats_per_session=30, takeover_share=0.20, retention_months=12)


def usermonth(rows):
    """dollars per active user-month for the STAGE-1H session shape (8 sessions of 30 minutes, a publish every 30 s,
    30 lease keepalives, 20% of sessions end in a move) from the measured per-operation counts."""
    g = group(rows)
    env = sorted({k[0] for k in g}, key=lambda e: (e != "same-box", e))[0]
    pr = PROFILE
    L = [f"## Dollars per active user-month ({env} counts; profile {pr['sessions']} sessions, {pr['publishes_per_session']} one-file publishes and "
         f"{pr['heartbeats_per_session']} keepalives each, {int(pr['takeover_share'] * 100)}% of sessions end in a move, one new repo a month, frames kept {pr['retention_months']} months)\n",
         "| Repo | First upload | Publishes | Keepalives | Moves, all cold | Moves, all warm (1 file) | Storage kept (steady state) | Month, cold moves | Month, warm moves |", "|---|---|---|---|---|---|---|---|---|"]
    hb = dollars({"requests": 1, "dir_requests": 1, "puts": 0, "gets": 0, "has": 0})
    for s in sorted({k[1] for k in g}, key=lambda s: ORDER[s]):
        f = lambda scen, kind: g.get((env, s, scen, kind), [])
        if not f("cold_take", "none"):
            continue
        first = med([x["usd"] for x in f("first_upload", "whole_repo")])
        pub = med([x["usd"] for x in f("publish_a", "one")])
        pub_b = med([x["bytes_up_payload"] for x in f("publish_a", "one")])
        cold, warm = med([x["usd"] for x in f("cold_take", "none")]), med([x["usd"] for x in f("warm_take", "one")])
        n_pub = pr["sessions"] * pr["publishes_per_session"]
        moves = pr["sessions"] * pr["takeover_share"]
        first_b = med([x["bytes_up_payload"] for x in f("first_upload", "whole_repo")])
        storage = (first_b + n_pub * pub_b) * pr["retention_months"] / 1e9 * P["gb_month"]
        ops_first = first - first_b / 1e9 * P["gb_month"]
        pub_ops = pub - pub_b / 1e9 * P["gb_month"]
        base = ops_first + n_pub * pub_ops + pr["sessions"] * pr["heartbeats_per_session"] * hb + storage
        L.append(f"| {s} | {usd(ops_first)} | {usd(n_pub * pub_ops)} | {usd(pr['sessions'] * pr['heartbeats_per_session'] * hb)} | {usd(moves * cold)} | {usd(moves * warm)} | {usd(storage)} | {usd(base + moves * cold)} | {usd(base + moves * warm)} |")
    return L + [""]


def md(rows, out):
    g = group(rows)
    envs = sorted({k[0] for k in g}, key=lambda e: (e != "same-box", e))
    sizes = sorted({k[1] for k in g}, key=lambda s: ORDER[s])
    sha = sorted({r["sha"] for r in rows})
    errors = [r for r in rows if r["scenario"] == "error"]
    rows = [r for r in rows if r["scenario"] != "error"]
    L = [f"# Move benchmark summary (product sha {', '.join(sha)}); median [min-max] of the reps\n"]

    def get(env, size, scen, kind):
        return g.get((env, size, scen, kind), [])

    rb = {s: next(iter(get(e, s, "first_upload", "whole_repo") or [{"repo_bytes": 0}]))["repo_bytes"] for e in envs for s in sizes}
    for e in envs:
        L += [f"## Headline, {e}\n", "| Repo | Tree | Cold take s | Cold $ | Warm take, 1 file s | Warm $ | Take-back, 1 file s | Publish 1 file, seal to durable s | Publish $ |", "|---|---|---|---|---|---|---|---|---|"]
        for s in sizes:
            cold, w1, tb, pub = get(e, s, "cold_take", "none"), get(e, s, "warm_take", "one"), get(e, s, "takeback", "one"), get(e, s, "publish_a", "one")
            if not cold:
                continue
            L.append(f"| {s} | {mb(rb[s])} MB | {cell([x['ms'] for x in cold], sec)} | {usd(med([x['usd'] for x in cold]))} | {cell([x['ms'] for x in w1], sec)} | {usd(med([x['usd'] for x in w1]))} | {cell([x['ms'] for x in tb], sec)} | {cell([x['ms'] for x in pub], sec)} | {usd(med([x['usd'] for x in pub]))} |")
        L.append("")
    for e in envs:
        L += [f"## Warm take and take-back by change size, {e}\n", "| Repo | Change | Files touched | Uploaded MB (A publish) | Publish s | Warm take s | GETs | Down MB | Take-back s | Take-back GETs |", "|---|---|---|---|---|---|---|---|---|---|"]
        for s in sizes:
            for k in ("one", "fifty", "big"):
                w, p, t = get(e, s, "warm_take", k), get(e, s, "publish_a", k), get(e, s, "takeback", k)
                if not w:
                    continue
                L.append(f"| {s} | {k} | {cell([x['changed'] for x in p])} | {cell([x['bytes_up_payload'] / 1e6 for x in p], lambda v: f'{v:,.1f}')} | {cell([x['ms'] for x in p], sec)} | {cell([x['ms'] for x in w], sec)} | {cell([x['gets'] for x in w])} | {cell([x['get_bytes'] / 1e6 for x in w], lambda v: f'{v:,.1f}')} | {cell([x['ms'] for x in t], sec)} | {cell([x['gets'] for x in t])} |")
        L.append("")
    for e in envs:
        L += [f"## Cold take and warm take (1 file) by repo size, {e}\n", "| Repo | Tree MB | Objects (GETs) cold | Down MB cold | Cold s | s per 1k GETs | Warm 1 file: GETs | Warm s | Warm/Cold time |", "|---|---|---|---|---|---|---|---|---|"]
        for s in sizes:
            c, w = get(e, s, "cold_take", "none"), get(e, s, "warm_take", "one")
            if not c:
                continue
            cg, cm = med([x["gets"] for x in c]), med([x["ms"] for x in c])
            L.append(f"| {s} | {mb(rb[s])} | {cg:,.0f} | {med([x['get_bytes'] for x in c]) / 1e6:,.1f} | {sec(cm)} | {cm / 1000 / (cg / 1000):,.2f} | {med([x['gets'] for x in w]):,.0f} | {sec(med([x['ms'] for x in w]))} | {med([x['ms'] for x in w]) / cm:.2f} |")
        L.append("")
    phases = ["directory", "engine_dirty", "engine_want", "fetch_get", "engine_import", "engine_materialize", "other"]
    for e in envs:
        L += [f"## Where a take spends its time (median ms; share), {e}\n", "| Repo | Take | Total | " + " | ".join(p.replace("engine_", "") for p in phases) + " |", "|---|---|---|" + "---|" * len(phases)]
        for s in sizes:
            for scen, kind, label in (("cold_take", "none", "cold"), ("warm_take", "one", "warm 1 file"), ("warm_take", "big", "warm +100 MB"), ("takeback", "one", "take-back 1 file")):
                xs = get(e, s, scen, kind)
                if not xs:
                    continue
                tot = med([x["ms"] for x in xs])
                cells = []
                for p in phases:
                    v = med([x["phases"].get(p, 0) for x in xs])
                    cells.append(f"{v:,.0f} ({100 * v / tot:.0f}%)")
                L.append(f"| {s} | {label} | {tot:,.0f} | " + " | ".join(cells) + " |")
        L.append("")
    for e in envs:
        L += [f"## Publish (seal to durable) by change size, {e}, by phase (median ms)\n", "| Repo | Change | Seal returns | Export | Frame PUT | Push total (seal returned to durable) | Durable | Requests | Puts | $ per publish |", "|---|---|---|---|---|---|---|---|---|---|"]
        for s in sizes:
            for k in ("one", "fifty", "big"):
                p = get(e, s, "publish_a", k)
                if not p:
                    continue
                m = lambda f: med([x[f] for x in p])
                L.append(f"| {s} | {k} | {m('seal_ms'):,.0f} | {m('export_ms'):,.0f} | {m('put_ms'):,.0f} | {m('push_ms'):,.0f} | {m('ms'):,.0f} | {m('requests'):,.0f} | {m('puts'):,.0f} | {usd(med([x['usd'] for x in p]))} |")
        L.append("")
    L += ["## Per tool call: seal returns, and seal to durable (ms; 1-file edit, back to back)\n", "| Env | Repo | Device | Sync interval | Seal returns median | p90 | Durable median | p90 | n |", "|---|---|---|---|---|---|---|---|---|"]
    for e in envs:
        for s in sizes:
            for who in ("a", "b"):
                for lab, nm in (("interval_200ms", "200 ms"), ("interval_default_5s", "5 s (default)")):
                    xs = [r for r in rows if r["env"] == e and r["size"] == s and r["scenario"] == "latency_" + lab and r["machine"] == device_name(rows, e, who)]
                    if not xs:
                        continue
                    sl, dl = sorted(r["turn"]["seal_ms"] for r in xs), sorted(r["turn"]["durable_ms"] for r in xs)
                    q = lambda v, p: v[min(len(v) - 1, int(p * len(v)))]
                    L.append(f"| {e} | {s} | {xs[0]['machine']} | {nm} | {statistics.median(sl):,.0f} | {q(sl, .9):,.0f} | {statistics.median(dl):,.0f} | {q(dl, .9):,.0f} | {len(xs)} |")
    L.append("")
    L += ["## First upload of the whole project (the first publish of a chat)\n", "| Env | Repo | Tree MB | Seal s | Push s | Durable s | Up MB | Objects | Requests | $ (incl. 1 month storage) |", "|---|---|---|---|---|---|---|---|---|---|"]
    for e in envs:
        for s in sizes:
            for x in get(e, s, "first_upload", "whole_repo"):
                L.append(f"| {e} | {s} | {mb(x['repo_bytes'])} | {sec(x['seal_ms'])} | {sec(x['push_ms'])} | {sec(x['ms'])} | {x['bytes_up_payload'] / 1e6:,.1f} | {x['objects']:,} | {x['requests']:,} | {usd(x['usd'])} |")
    L.append("")
    for e in envs[:1]:
        L += [f"## Operations and dollars per move (counts are the same in every environment; {e})\n", "| Repo | Move | Worker requests | Directory (DO) requests | R2 Class A (puts) | R2 Class B (gets) | Bytes up MB | Bytes down MB | $ ops | $ storage, 1 month |", "|---|---|---|---|---|---|---|---|---|---|"]
        for s in sizes:
            for scen, kind, label in (("first_upload", "whole_repo", "first upload"), ("cold_take", "none", "cold take"), ("warm_take", "one", "warm take, 1 file"), ("warm_take", "fifty", "warm take, 50 files"), ("warm_take", "big", "warm take, +100 MB"), ("takeback", "one", "take-back, 1 file"), ("publish_a", "one", "publish, 1 file"), ("publish_a", "big", "publish, +100 MB")):
                xs = get(e, s, scen, kind)
                if not xs:
                    continue
                m = lambda f: med([x[f] for x in xs])
                up = m("put_bytes") if "put_bytes" in xs[0] else 0
                stor = up / 1e9 * P["gb_month"] if scen in ("first_upload", "publish_a") else 0
                L.append(f"| {s} | {label} | {m('requests'):,.0f} | {m('dir_requests'):,.0f} | {m('puts'):,.0f} | {m('gets'):,.0f} | {up / 1e6:,.1f} | {m('get_bytes') / 1e6:,.1f} | {usd(m('usd') - stor)} | {usd(stor)} |")
        L.append("")
    L += usermonth(rows)
    if errors:
        L += ["## Steps that failed (kept out of the medians)\n", "| Env | Repo | Change | Rep | Error |", "|---|---|---|---|---|"]
        L += [f"| {r['env']} | {r['size']} | {r['kind']} | {r['rep']} | {r['error'][:160].replace('|', '/')} |" for r in errors]
        L.append("")
    (Path(out) / "summary.md").write_text("\n".join(L))


if __name__ == "__main__":
    out = sys.argv[1]
    rows = load(out)
    json.dump({"prices": P, "cells": summary(rows)}, open(Path(out) / "summary.json", "w"), indent=1)
    md(rows, out)
