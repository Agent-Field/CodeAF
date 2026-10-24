#!/usr/bin/env python3
"""Bill per Cloudflare line item at 1k, 10k and 100k users (25% active), for any number of billed runs.

Reuses bill.py's month arithmetic (one source of truth for the shape and the prices) and adds the two lines
bill.py leaves out: R2 storage, priced from the frame puts the client sends, and WebSocket hibernation,
shown as the share of the open-home seconds the Durable Object was billed active.

  python3 lines.py --column TITLE MANIFEST BILLED [--column ...]
"""
import argparse

import bill
from model import PRICE, over

SCALES = [(1_000, 0.25), (10_000, 0.25), (100_000, 0.25)]
FRAME_BYTES = 43_200  # model.Shape.flush_bytes: the measured mean frame
LINES = ["Worker requests", "Worker CPU ms", "DO requests", "DO GB-s", "DO rows read", "DO rows written",
         "R2 Class A", "R2 Class B"]


def frame_puts(manifest, shape):
    """Frame puts per user-month, from each move phase's own count of puts per move."""
    phases = {p["name"]: p for p in manifest["phases"]}
    per_move = lambda n: phases[n].get("client_requests", {}).get("POST /v1/store/frames", 0) / phases[n]["count"]
    return shape["warm_per_month"] * per_move("warm_moves") + shape["cold_per_month"] * per_move("cold_moves")


def storage_dollars(puts, users):
    """One month of retained frames; the first 10 GB are free."""
    gb = puts * FRAME_BYTES * users / 1e9
    return over(gb * 1e6, PRICE["r2_gb_free"] * 1e6, PRICE["r2_gb_month"]), gb


def awake_share(manifest, billed, shape):
    """Billed active seconds over the open-home seconds in the steady phase: what hibernation leaves awake."""
    idle = next(p for p in manifest["phases"] if p["name"] == "idle_hold")
    return billed["phases"]["idle_hold"]["do_active_s"] / idle["seconds"]


def column(c, shape):
    month = bill.month_usage(c["manifest"], c["billed"], shape)
    puts = frame_puts(c["manifest"], shape)
    rows = {}
    for n, active in SCALES:
        m = {name: d for name, _, _, d in bill.meters(bill.scale(month, n * active), 1, 1)}
        storage, gb = storage_dollars(puts, n * active)
        rows[n] = {**m, "R2 storage": storage, "_base": m["Workers base"], "_gb": gb}
    return month, rows, awake_share(c["manifest"], c["billed"], shape)


def main(argv=None):
    ap = argparse.ArgumentParser()
    ap.add_argument("--column", nargs=3, action="append", required=True, metavar=("TITLE", "MANIFEST", "BILLED"))
    a = ap.parse_args(argv)
    cols = [dict(title=t, manifest=bill.load(m), billed=bill.load(b)) for t, m, b in a.column]
    shape = bill.shape_for(cols[0]["manifest"], {})
    out = [column(c, shape) for c in cols]
    names = LINES + ["R2 storage"]
    print("Usage per user-month")
    for label, k in bill.USAGE_ROWS:
        print(f"| {label} | " + " | ".join(bill.num(m[k]) for m, _, _ in out) + " |")
    print("| DO active s (billed) | " + " | ".join(bill.num(m["do_active_s"]) for m, _, _ in out) + " |")
    print("| awake share of open-home time | " + " | ".join(f"{s * 100:.2f}%" for _, _, s in out) + " |")
    for n, _ in SCALES:
        print(f"\nDollars at {n:,} users, 25% active")
        print("| Line | " + " | ".join(c["title"] for c in cols) + " |")
        for name in ["Workers base"] + names:
            key = "_base" if name == "Workers base" else name
            print(f"| {name} | " + " | ".join(f"${r[n][key]:,.2f}" for _, r, _ in out) + " |")
        print("| **Total** | " + " | ".join(f"**${sum(r[n][k] for k in ['_base'] + names):,.2f}**" for _, r, _ in out) + " |")
        print("| frames stored GB | " + " | ".join(f"{r[n]['_gb']:.2f}" for _, r, _ in out) + " |")


if __name__ == "__main__":
    main()
