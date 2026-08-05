#!/usr/bin/env python3
"""Measure whether the routed arm's decisions shifted between run 1 and run 3.

This is the continual-learning check from DESIGN.md §6. It is written before
the router exists, so it deliberately does not assume a schema: `collect.py`
snapshots every file under the profile directory after every cell, and this
script diffs those snapshots plus whatever per-leaf routing information ended
up in the rows.

Three things are reported, in increasing order of how much they would mean:

  1  **ledger growth** -- did the shared ledger actually accumulate? A learning
     claim over three runs that left the ledger byte-identical is not a
     learning claim, it is a plumbing bug, and this catches it first.
  2  **decision diff** -- for tasks seen in both run 1 and run 3, which leaves
     were routed to a different model, and in which direction along the panel's
     price ordering. Escalating less on a task the panel has already seen is
     the shape learning should take; escalating *more* is worth knowing too.
  3  **outcome at constant task** -- score, cost, escalation rate and turns for
     the same task in run 1 versus run 3. Learning that moves neither score nor
     cost is bookkeeping.

The fresh-ledger control matters and is checked for: without a `LEDGER_MODE=fresh`
arm-B run to compare against, a run-1/run-3 difference cannot be separated from
ordinary run-to-run variance, and this script says so rather than implying a
result it cannot support.

Usage:
  python3 learning-diff.py results-armB.jsonl [--control results-armB-fresh.jsonl]
"""
import argparse
import json
import os
import sys

# Panel price order, cheapest first. Used only to say whether a changed
# decision moved up-rung or down-rung; it is read from panel.json so it cannot
# drift away from the panel actually used.
HERE = os.path.dirname(os.path.abspath(__file__))


def panel_order():
    with open(os.path.join(HERE, "panel.json")) as f:
        panel = json.load(f)["panel"]
    ordered = sorted(panel, key=lambda m: m["price_out_per_mtok"])
    return {m["slug"]: n for n, m in enumerate(ordered)}, \
           {m["slug"]: m["label"] for m in panel}


def load(path):
    rows = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if line:
                rows.append(json.loads(line))
    return rows


def routing_decisions(row):
    """Pull per-leaf model assignments out of a row, wherever the router put them.

    Tries the places a router plausibly records a decision, in order, and
    returns {leaf_id: slug}. An empty result is reported rather than guessed
    at: it means the router is not writing decisions where this can see them,
    which is itself the finding.
    """
    out = {}
    for leaf in row.get("per_leaf") or []:
        for key in ("model", "routed_model", "slug"):
            if leaf.get(key):
                out[leaf["id"]] = leaf[key]
                break
    if out:
        return out, "per_leaf"
    # a routing-events file in the ledger snapshot
    files = ((row.get("ledger") or {}).get("files") or {})
    for name, entry in files.items():
        if "rout" not in name.lower():
            continue
        blob = entry.get("json")
        if isinstance(blob, list):
            for event in blob:
                if isinstance(event, dict) and "node" in event and "model" in event:
                    out[event["node"]] = event["model"]
        elif isinstance(blob, dict):
            for node, model in (blob.get("decisions") or {}).items():
                out[node] = model
        if out:
            return out, name
    return {}, None


def ledger_fingerprint(row):
    files = ((row.get("ledger") or {}).get("files") or {})
    return {name: entry.get("bytes") for name, entry in sorted(files.items())}


def models_from_profiles(row):
    """Which models the run actually used, read off the profile store.

    aforge writes one profile file per (model, skill) -- the calibration cells
    produced `profile-deepseek-deepseek-v4-flash-latest-linear.json` and nothing
    else, because arm A uses one model. Under routing the *set of files* is
    therefore direct evidence of the panel members that were exercised, and the
    record count inside each is how much work each one was given.

    This works whether or not the router logs a routing event, which is why it
    is here: the learning check should not be blind because a schema was named
    differently than expected.
    """
    out = {}
    for name, entry in ((row.get("ledger") or {}).get("files") or {}).items():
        blob = entry.get("json")
        if not isinstance(blob, dict) or "model" not in blob:
            continue
        out[blob["model"]] = len(blob.get("records") or [])
    return out


def report(rows, label):
    by_task_rep = {(r["task"], r["rep"]): r for r in rows}
    reps = sorted({r["rep"] for r in rows})
    tasks = sorted({r["task"] for r in rows})
    if len(reps) < 2:
        print(f"{label}: only {len(reps)} replicate(s) — nothing to diff")
        return
    first, last = reps[0], reps[-1]
    order, labels = panel_order()

    print(f"== {label}: run {first} vs run {last} ==\n")

    # 1. did the ledger move at all?
    print("ledger growth (bytes after each run, per task):")
    for task in tasks:
        sizes = []
        for rep in reps:
            row = by_task_rep.get((task, rep))
            sizes.append((row.get("ledger") or {}).get("bytes", 0) if row else None)
        print(f"  {task:<15} {sizes}")
    grew = any(
        ledger_fingerprint(by_task_rep[(t, last)]) != ledger_fingerprint(by_task_rep[(t, first)])
        for t in tasks if (t, first) in by_task_rep and (t, last) in by_task_rep)
    print(f"  -> ledger changed between run {first} and run {last}: {grew}")
    if not grew:
        print("  !! a shared-ledger arm whose ledger did not change is a plumbing"
              " bug, not a null result — check AFORGE_PROFILE_DIR is shared")
    print()

    # 2. decision diff
    print("routing decisions:")
    any_decisions = False
    for task in tasks:
        a = by_task_rep.get((task, first))
        b = by_task_rep.get((task, last))
        if not (a and b):
            continue
        da, source_a = routing_decisions(a)
        db, _ = routing_decisions(b)
        if not da and not db:
            print(f"  {task:<15} no per-leaf model recorded — the router is not "
                  f"writing decisions anywhere collect.py can see them")
            continue
        any_decisions = True
        shared = sorted(set(da) & set(db))
        changed = [(n, da[n], db[n]) for n in shared if da[n] != db[n]]
        up = sum(1 for _n, x, y in changed if order.get(y, 0) > order.get(x, 0))
        down = len(changed) - up
        print(f"  {task:<15} source={source_a}  {len(shared)} comparable leaves, "
              f"{len(changed)} changed ({up} up-rung, {down} down-rung)")
        for node, x, y in changed:
            print(f"      leaf {node}: {labels.get(x, x)} -> {labels.get(y, y)}")
    if not any_decisions:
        print("  -> no routing decisions found in either run; the decision diff "
              "cannot be computed from per-leaf records")
    print()

    # Fallback and cross-check: the profile store names one file per model, so
    # the set of files says which panel members were exercised even when no
    # routing event was logged.
    print("models exercised, per the profile store (model: leaves recorded):")
    for task in tasks:
        for rep in (first, last):
            row = by_task_rep.get((task, rep))
            if not row:
                continue
            used = models_from_profiles(row)
            pretty = ", ".join(f"{labels.get(m, m)}:{n}" for m, n in sorted(used.items()))
            print(f"  {task:<15} run {rep}  {pretty or '(none recorded)'}")
    print()

    # 3. outcome at constant task
    print(f"{'task':<15} {'score 1':>8} {'score N':>8} {'$ 1':>9} {'$ N':>9} "
          f"{'turns 1':>8} {'turns N':>8} {'calls 1':>8} {'calls N':>8}")
    for task in tasks:
        a = by_task_rep.get((task, first))
        b = by_task_rep.get((task, last))
        if not (a and b):
            continue
        print(f"{task:<15} {a['score']:>8.3f} {b['score']:>8.3f} "
              f"{a['cost_usd']:>9.4f} {b['cost_usd']:>9.4f} "
              f"{a['turns']:>8} {b['turns']:>8} {a['calls']:>8} {b['calls']:>8}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("jsonl")
    ap.add_argument("--control", default=None,
                    help="a LEDGER_MODE=fresh arm-B run, without which a run-1/"
                         "run-3 difference cannot be separated from variance")
    args = ap.parse_args()

    rows = load(args.jsonl)
    if not rows:
        print("no rows", file=sys.stderr)
        sys.exit(1)
    modes = {r.get("ledger_mode") for r in rows}
    if modes != {"shared"}:
        print(f"warning: ledger_mode is {modes}, expected {{'shared'}} — the "
              f"learning check assumes the ledger carried forward\n")
    report(rows, "shared ledger")

    if args.control:
        print("\n")
        report(load(args.control), "fresh ledger (control)")
        print("\nRead the two together: a run-1/run-3 shift that also appears in "
              "the fresh-ledger control is run-to-run variance, not learning.")
    else:
        print("\n!! no fresh-ledger control supplied. Any difference above is "
              "confounded with ordinary run-to-run variance — aforge's planner "
              "samples the spine three times and leaf order varies. Run\n"
              "     ARM=b LEDGER_MODE=fresh JSONL=results-armB-fresh.jsonl ./run-arm.sh\n"
              "   and pass it with --control before claiming learning.")


if __name__ == "__main__":
    main()
