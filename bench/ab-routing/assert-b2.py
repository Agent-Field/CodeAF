#!/usr/bin/env python3
"""The Phase B2 assertions from DESIGN.md §11.3.

Each one is a defect from REPORT.md §5 turned into something the harness can
check, so that the next run fails loudly on the first cell rather than silently
until someone reads nine cells of event log. Phase B's collapse was invisible
until the whole arm was analysed; none of these would have let it be.

They are properties of the *run*, not of the model: a failure here means the
router is behaving differently from how the fixes intend, not that a task was
hard.

Usage:
  python3 assert-b2.py results-armB2.jsonl [--events events-armB2.jsonl]
                       [--ledger ledger-armB2-shared/router-ledger.json]
                       [--min-graded 5] [--json]
"""
import argparse
import json
import os
import sys
from collections import defaultdict

HERE = os.path.dirname(os.path.abspath(__file__))
HARD_TASKS = {"t1-logstore", "t3-shiftplan", "t4-pathmatch"}


def panel():
    with open(os.path.join(HERE, "panel.json")) as f:
        models = json.load(f)["models"]
    return ({m["slug"]: m.get("label", m["slug"]) for m in models},
            {m["slug"] for m in models},
            next((m["slug"] for m in models if m.get("role") == "top"), None),
            {m["slug"]: m.get("role", "") for m in models})


LABEL, PANEL, TOP, ROLE = panel()

# internal/router/panel.go coldStart: top +1, base -1, anything else 0.
COLD = {"top": 1.0, "base": -1.0}


def rating_of(rated, slug, cls):
    """What the router believes about this model for this class, right now.

    Falling back to the cold-start prior is the point rather than a
    convenience. Tested against Phase B's data, A1 and A3 passed on the very run
    they were written to catch: an unmeasured model has no ledger row,
    `rated.get` returned None, and the comparison was skipped. But an unmeasured
    model is exactly what the router was ordering against — it uses
    coldStart(role) for it — so skipping it made the check vacuous in precisely
    the case that matters.
    """
    found = rated.get((slug, cls))
    if found is not None:
        return found[0], found[1]
    stem = slug.split("/")[-1].split("-latest")[0]
    for (model, other_class), (value, count) in rated.items():
        if other_class == cls and stem in model:
            return value, count
    return COLD.get(ROLE.get(slug, ""), 0.0), 0


def short(slug):
    return LABEL.get(slug, slug.split("/")[-1])


def load_rows(path):
    rows = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if line:
                rows.append(json.loads(line))
    return rows


def load_events(rows, explicit):
    """Per-cell events, from the rows or from the companion file beside them."""
    table = {}
    if explicit and os.path.exists(explicit):
        with open(explicit) as f:
            for line in f:
                line = line.strip()
                if line:
                    item = json.loads(line)
                    table[(item["task"], item["rep"])] = item["events"]
    out = []
    for row in rows:
        inline = (row.get("ledger") or {}).get("events") or []
        events = inline or table.get((row["task"], row["rep"]), [])
        out.append((row, events))
    return out


def ratings(ledger_path):
    """(model, class) -> (rating, count), from the router's own ledger."""
    if not ledger_path or not os.path.exists(ledger_path):
        return {}
    with open(ledger_path) as f:
        blob = json.load(f)
    out = {}
    for entry in blob.get("entries") or []:
        out[(entry["model"], entry["class"])] = (entry["rating"], entry["count"])
    return out


class Checks:
    def __init__(self):
        self.results = []

    def add(self, name, ok, detail="", advisory=False):
        self.results.append({"assertion": name, "ok": bool(ok),
                             "detail": detail, "advisory": advisory})

    def failed(self):
        return [r for r in self.results if not r["ok"] and not r["advisory"]]


def run(rows, events, rated, min_graded):
    checks = Checks()

    attempts = [(row, e) for row, evs in events for e in evs if not e.get("final")]

    # A1: the terminal rung is never weaker than rung 0.
    bad = []
    for row, e in attempts:
        candidates = e.get("candidates") or []
        if len(candidates) < 2:
            continue
        first = rating_of(rated, candidates[0], e["class"])
        last = rating_of(rated, candidates[-1], e["class"])
        if last[0] < first[0] - 1e-9:
            bad.append(f"{e['class']}: {short(candidates[-1])} "
                       f"({last[0]:+.2f}) terminal under {short(candidates[0])} "
                       f"({first[0]:+.2f})")
    checks.add("A1 terminal rung is never weaker than rung 0", not bad,
               "; ".join(sorted(set(bad))[:4]))

    # A2: the top model is reached on a hard task that failed.
    reached = set()
    for row, e in attempts:
        if row["task"] in HARD_TASKS and not row["success"] and e.get("rung", 0) > 0:
            reached.add(e.get("model"))
    checks.add(f"A2 {short(TOP) if TOP else 'the top rung'} is reached by an "
               f"escalation on a failed hard task",
               TOP in reached,
               f"models reached above rung 0 on failed hard cells: "
               f"{sorted(short(m) for m in reached) or 'none'}")

    # A3: no escalation lands on a model rated below the one it left.
    bad = []
    for row, e in attempts:
        if e.get("rung", 0) == 0:
            continue
        candidates = e.get("candidates") or []
        if not candidates:
            continue
        came_from = candidates[0]
        to = rating_of(rated, e.get("model"), e["class"])
        frm = rating_of(rated, came_from, e["class"])
        if to[0] < frm[0] - 1e-9:
            bad.append(f"{row['task']}: {short(came_from)} ({frm[0]:+.2f}) -> "
                       f"{short(e.get('model'))} ({to[0]:+.2f})")
    checks.add("A3 no escalation lands on a weaker model", not bad,
               "; ".join(sorted(set(bad))[:4]))

    # A4: every panel member has been observed at all.
    observed = {model for (model, _cls) in rated}
    # the ledger keys on the resolved snapshot, so match on the tail
    seen = {slug for slug in PANEL
            if any(slug.split("/")[-1].split("-latest")[0] in m for m in observed)}
    checks.add("A4 every panel member has at least one observation",
               len(seen) == len(PANEL),
               f"unobserved: {sorted(short(s) for s in PANEL - seen)}")

    # A5: a rating with too little graded evidence must not reorder anything.
    orders = defaultdict(set)
    members = defaultdict(set)
    for _row, e in attempts:
        if e.get("candidates"):
            orders[e["class"]].add(tuple(e["candidates"]))
            members[e["class"]].update(e["candidates"])
    reordered = {cls for cls, seen_orders in orders.items() if len(seen_orders) > 1}
    # A reorder is only earned if every model whose position moved has enough
    # graded evidence behind it. Phase B reordered exec.leaf on five
    # observations of one model against zero of every other.
    unearned = []
    for cls in sorted(reordered):
        for slug in sorted(members[cls]):
            count = rating_of(rated, slug, cls)[1]
            if count < min_graded:
                unearned.append(f"{cls}: {short(slug)} n={count}")
    checks.add(f"A5 no class reorders while a model in it has < "
               f"{min_graded} graded observations",
               not unearned, "; ".join(unearned[:6]))

    # A6: leaf ratings are conditioned on something.
    leaf_keys = [key for key in rated if key[1].startswith("exec.leaf")]
    distinct = {key[1] for key in leaf_keys}
    checks.add("A6 exec.leaf ratings are conditioned, not one global class",
               len(distinct) > 1,
               f"leaf classes in the ledger: {sorted(distinct) or 'none'}")

    # A7: a leaf escalation records its chain.
    missing = sum(1 for _row, e in attempts
                  if e.get("rung", 0) > 0 and not e.get("escalation"))
    checks.add("A7 every escalation records its chain", missing == 0,
               f"{missing} attempts above rung 0 with an empty `escalation`")

    return checks


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("results")
    ap.add_argument("--events", default=None)
    ap.add_argument("--ledger", default=None)
    ap.add_argument("--min-graded", type=int, default=5)
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args()

    rows = load_rows(args.results)
    events = load_events(rows, args.events or
                         args.results.replace("results-", "events-"))
    rated = ratings(args.ledger)
    if not rated:
        print("warning: no ledger supplied, so A1/A3/A4/A5/A6 cannot be "
              "evaluated against ratings\n", file=sys.stderr)

    checks = run(rows, events, rated, args.min_graded)
    if args.json:
        print(json.dumps(checks.results, indent=2))
    else:
        for item in checks.results:
            mark = "PASS" if item["ok"] else ("warn" if item["advisory"] else "FAIL")
            print(f"  {mark}  {item['assertion']}")
            if item["detail"]:
                print(f"         {item['detail']}")
    failed = checks.failed()
    print(f"\n{len(checks.results) - len(failed)}/{len(checks.results)} assertions passed")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
