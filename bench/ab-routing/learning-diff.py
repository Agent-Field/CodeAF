#!/usr/bin/env python3
"""Did the router's decisions shift between run 1 and run 3?

This is the continual-learning check from DESIGN.md §6, rewritten against the
router's actual event log now that it exists. `router-events.jsonl` is richer
than the profile-file-set fallback the design planned for, and one field is the
reason: **`candidates`** is the ordered rung list the choice was made from. Both
Phase A labs had to reconstruct what a router *would* have done from an offline
matrix and neither could recover the counterfactual. Here it is recorded, so
"the router changed its mind" is a diff rather than an inference.

Five measurements, in increasing order of how much they would mean:

  1  **ledger growth** — did the shared ledger accumulate at all? A learning
     claim over three runs that left it byte-identical is a plumbing bug
     wearing a null result, and this catches it before anything else.
  2  **rung order** — the `candidates` list per call class, run 1 vs run 3.
     This is the decision the ledger exists to move.
  3  **who opened the call** — the rung-0 model per class, and each model's
     share of attempts.
  4  **escalation and verdicts** — how often the cascade fired, and how calls
     ended.
  5  **outcome at constant task** — score, cost and turns for the same task in
     run 1 vs run 3. Learning that moves none of them is bookkeeping.

The fresh-ledger control is what separates learning from run-to-run variance:
aforge samples the spine three times and leaf order varies, so a run-1/run-3
difference means nothing on its own. Without a control this script says so
rather than implying a result it cannot support.

Usage:
  python3 learning-diff.py results-armB.jsonl [--control results-armB-fresh.jsonl]
"""
import argparse
import json
import os
import sys
from collections import Counter, defaultdict

HERE = os.path.dirname(os.path.abspath(__file__))


def panel_labels():
    with open(os.path.join(HERE, "panel.json")) as f:
        panel = json.load(f)["models"]
    return ({m["slug"]: m.get("label", m["slug"].split("/")[-1]) for m in panel},
            [m["slug"] for m in panel])


LABEL, PANEL_ORDER = panel_labels()


def short(slug):
    return LABEL.get(slug, slug.split("/")[-1])


def load(path):
    """Rows, each stamped with which results file it came from.

    The stamp matters: the shared arm and its control have the same (task,
    replicate) keys, so a companion lookup that searched every loaded table
    would answer the control with the shared arm's events and report the two as
    identical — which is exactly the conclusion the control exists to test.
    """
    table = load_companion(path)
    rows = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if line:
                row = json.loads(line)
                row["_events"] = table.get((row["task"], row["rep"]), [])
                rows.append(row)
    return rows


_COMPANION = {}


def load_companion(results_path):
    """Per-cell events, from the file beside the results.

    The events are the substrate of this whole analysis, but inlining them into
    every results row duplicated one log nine times over. They live in
    `events-<arm>.jsonl` keyed by (task, replicate); a row that still carries
    its own inline list is used as-is, so both layouts work.
    """
    name = os.path.basename(results_path).replace("results-", "events-")
    path = os.path.join(os.path.dirname(os.path.abspath(results_path)), name)
    table = {}
    if os.path.exists(path):
        with open(path) as f:
            for line in f:
                line = line.strip()
                if line:
                    row = json.loads(line)
                    table[(row["task"], row["rep"])] = row["events"]
    _COMPANION[results_path] = table
    return table


def all_events(row):
    inline = (row.get("ledger") or {}).get("events") or []
    return inline or row.get("_events") or []


def events_of(row):
    """Attempt rows only. A `final` row is the settled verdict appended against
    an existing call id and carries no fresh decision."""
    return [e for e in all_events(row) if not e.get("final")]


def finals_of(row):
    return [e for e in all_events(row) if e.get("final")]


def rung_orders(rows):
    out = defaultdict(Counter)
    for row in rows:
        for e in events_of(row):
            if e.get("candidates"):
                out[e["class"]][" > ".join(short(c) for c in e["candidates"])] += 1
    return out


def openers(rows):
    out = defaultdict(Counter)
    for row in rows:
        for e in events_of(row):
            if e.get("rung", 0) == 0:
                out[e["class"]][short(e["model"])] += 1
    return out


def model_share(rows):
    out = Counter()
    for row in rows:
        for e in events_of(row):
            out[short(e["model"])] += 1
    return out


def escalation_stats(rows):
    calls, escalated, chains = set(), set(), Counter()
    for row in rows:
        for e in events_of(row):
            calls.add(e.get("call"))
            if e.get("escalation"):
                escalated.add(e.get("call"))
                chains[" -> ".join(short(x) for x in e["escalation"])
                       + " -> " + short(e["model"])] += 1
    return {"calls": len(calls), "escalated": len(escalated),
            "rate": (len(escalated) / len(calls)) if calls else 0.0,
            "chains": chains}


def verdict_mix(rows):
    out = Counter()
    for row in rows:
        for e in finals_of(row):
            out[e.get("verdict", "?")] += 1
        for e in events_of(row):
            v = e.get("verdict")
            if v and v != "unverified_success":
                out["attempt:" + v] += 1
    return out


def report(rows, label):
    reps = sorted({r["rep"] for r in rows})
    tasks = sorted({r["task"] for r in rows})
    if len(reps) < 2:
        print(f"{label}: only {len(reps)} replicate(s) — nothing to diff")
        return None
    first, last = reps[0], reps[-1]
    early = [r for r in rows if r["rep"] == first]
    late = [r for r in rows if r["rep"] == last]

    print(f"\n{'=' * 74}\n== {label}: run {first} vs run {last}\n{'=' * 74}")

    # 1. did the ledger move at all?
    print("\n--- 1. ledger growth ---")
    print(f"{'task':<15} {'bytes r' + str(first):>11} {'bytes r' + str(last):>11} "
          f"{'events r' + str(first):>11} {'events r' + str(last):>11}")
    moved = False
    for task in tasks:
        a = next((r for r in early if r["task"] == task), None)
        b = next((r for r in late if r["task"] == task), None)
        if not (a and b):
            continue
        ab = (a.get("ledger") or {}).get("bytes", 0)
        bb = (b.get("ledger") or {}).get("bytes", 0)
        print(f"{task:<15} {ab:>11} {bb:>11} {len(events_of(a)):>11} {len(events_of(b)):>11}")
        moved = moved or bb != ab
    print(f"  -> ledger grew between run {first} and run {last}: {moved}")
    if not moved:
        print("  !! a shared-ledger arm whose ledger did not grow is a plumbing "
              "bug, not a null result — check AFORGE_PROFILE_DIR is shared")

    # 2. the rung order — the decision the ledger exists to move
    print("\n--- 2. rung order (the `candidates` list at decision time) ---")
    ra, rb = rung_orders(early), rung_orders(late)
    classes = sorted(set(ra) | set(rb))
    changed = []
    for cls in classes:
        oa = ra[cls].most_common(1)[0][0] if ra[cls] else "(none)"
        ob = rb[cls].most_common(1)[0][0] if rb[cls] else "(none)"
        mark = "   <-- CHANGED" if oa != ob and "(none)" not in (oa, ob) else ""
        if mark:
            changed.append(cls)
        print(f"  {cls:<16} r{first}: {oa}")
        print(f"  {'':<16} r{last}: {ob}{mark}")
    print(f"  -> {len(changed)} of {len(classes)} call classes reordered: "
          f"{changed or 'none'}")

    # 3. who opened the call
    print("\n--- 3. opening model, and share of all attempts ---")
    oa, ob = openers(early), openers(late)
    for cls in sorted(set(oa) | set(ob)):
        print(f"  {cls:<16} r{first}: {dict(oa[cls])}   r{last}: {dict(ob[cls])}")
    sa, sb = model_share(early), model_share(late)
    print(f"  share  r{first}: {dict(sa.most_common())}")
    print(f"  share  r{last}: {dict(sb.most_common())}")
    print(f"  panel members exercised: r{first} {len(sa)}/{len(PANEL_ORDER)}, "
          f"r{last} {len(sb)}/{len(PANEL_ORDER)}")

    # 4. escalation and verdicts
    print("\n--- 4. escalation and verdicts ---")
    ea, eb = escalation_stats(early), escalation_stats(late)
    for tag, st in ((first, ea), (last, eb)):
        print(f"  run {tag}: {st['escalated']}/{st['calls']} calls escalated "
              f"({st['rate']:.1%})")
        for chain, n in st["chains"].most_common():
            print(f"      {n}x  {chain}")
    print(f"  verdicts r{first}: {dict(verdict_mix(early).most_common())}")
    print(f"  verdicts r{last}: {dict(verdict_mix(late).most_common())}")

    # 5. outcome at constant task
    print("\n--- 5. outcome at constant task ---")
    print(f"{'task':<15} {'score r' + str(first):>9} {'score r' + str(last):>9} "
          f"{'$ r' + str(first):>9} {'$ r' + str(last):>9} "
          f"{'turns r' + str(first):>9} {'turns r' + str(last):>9}")
    for task in tasks:
        a = next((r for r in early if r["task"] == task), None)
        b = next((r for r in late if r["task"] == task), None)
        if not (a and b):
            continue
        print(f"{task:<15} {a['score']:>9.3f} {b['score']:>9.3f} "
              f"{a['cost_usd']:>9.4f} {b['cost_usd']:>9.4f} "
              f"{a['turns']:>9} {b['turns']:>9}")

    return {"reordered_classes": changed, "ledger_grew": moved,
            "escalation_first": ea, "escalation_last": eb,
            "share_first": sa, "share_last": sb}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("jsonl")
    ap.add_argument("--control", default=None,
                    help="a LEDGER_MODE=fresh arm-B run, without which a "
                         "run-1/run-3 difference cannot be separated from variance")
    args = ap.parse_args()

    rows = load(args.jsonl)
    if not rows:
        print("no rows", file=sys.stderr)
        sys.exit(1)
    modes = {r.get("ledger_mode") for r in rows}
    if modes != {"shared"}:
        print(f"warning: ledger_mode is {modes}, expected {{'shared'}}\n")
    shared = report(rows, "shared ledger")

    if not args.control:
        print("\n!! no fresh-ledger control supplied. Any difference above is "
              "confounded with ordinary run-to-run variance — aforge samples the "
              "spine three times and leaf order varies. Run\n"
              "     ARM=b LEDGER_MODE=fresh JSONL=results-armB-fresh.jsonl ./run-arm.sh\n"
              "   and pass it with --control before claiming learning.")
        return

    control = report(load(args.control), "fresh ledger (control)")

    print(f"\n{'=' * 74}\n== verdict\n{'=' * 74}")
    sc = set(shared["reordered_classes"]) if shared else set()
    cc = set(control["reordered_classes"]) if control else set()
    attributable = sc - cc
    print(f"  reordered with a shared ledger : {sorted(sc) or 'none'}")
    print(f"  reordered with a fresh ledger  : {sorted(cc) or 'none'}")
    print(f"  attributable to the ledger     : {sorted(attributable) or 'none'}")
    if not sc:
        print("\n  The router did not change its mind between run 1 and run 3.")
        print("  That is a real answer rather than a missing one: with this panel,")
        print("  this suite and three runs, the ledger did not accumulate enough")
        print("  graded evidence to reorder anything.")
    elif not attributable:
        print("\n  Every reordering also happened without a ledger, so it is "
              "run-to-run variance rather than learning.")
    else:
        print("\n  These classes reordered only when the ledger carried forward, "
              "which is learning.")


if __name__ == "__main__":
    main()
