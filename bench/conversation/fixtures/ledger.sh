#!/usr/bin/env bash
# ledger.sh — the data workload's fixture: a small CSV with arithmetic in it.
#
# Deterministic and offline. The point of a data cell is that the right answer
# is a NUMBER, computed from the same bytes the harness was given, so the check
# is arithmetic rather than judgement — and so a fluent, confident, wrong answer
# fails. The expected values are computed here, from the fixture, and written
# beside it: a benchmark whose answer key is typed by hand drifts away from its
# own fixture the first time the fixture is edited.

fixture_ledger() {
  local work="$1"
  mkdir -p "$work"
  cat > "$work/ledger.csv" <<'CSV'
date,region,product,units,unit_price_usd,refunded
2026-01-04,north,widget,12,19.50,no
2026-01-07,south,widget,5,19.50,yes
2026-01-11,north,gasket,40,3.25,no
2026-01-15,east,widget,7,19.50,no
2026-01-19,south,gasket,18,3.25,no
2026-02-02,east,flange,3,127.00,no
2026-02-08,north,widget,22,19.50,no
2026-02-14,west,gasket,60,3.25,yes
2026-02-21,east,gasket,15,3.25,no
2026-03-03,north,flange,2,127.00,no
2026-03-09,south,widget,9,19.50,no
2026-03-17,west,widget,11,19.50,no
CSV

  # The answer key, computed from the file just written. Revenue counts only the
  # rows that were not refunded, which is the one place a careless reader of the
  # question can go wrong and the reason the question is worth asking.
  python3 - "$work" <<'PY'
import csv, json, sys, collections
work = sys.argv[1]
rows = list(csv.DictReader(open(work + "/ledger.csv")))
kept = [r for r in rows if r["refunded"] == "no"]
revenue = sum(int(r["units"]) * float(r["unit_price_usd"]) for r in kept)
by_region = collections.Counter()
for r in kept:
    by_region[r["region"]] += int(r["units"]) * float(r["unit_price_usd"])
top_region, top_revenue = by_region.most_common(1)[0]
json.dump({
    "net_revenue_usd": round(revenue, 2),
    "top_region": top_region,
    "top_region_revenue_usd": round(top_revenue, 2),
    "refunded_rows": len(rows) - len(kept),
    "units_sold_net": sum(int(r["units"]) for r in kept),
}, open(work + "/expected.json", "w"), indent=1, sort_keys=True)
PY
}
