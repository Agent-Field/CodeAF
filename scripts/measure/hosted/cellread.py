#!/usr/bin/env python3
"""Read a cell's own on-disk record into JSON: `cellread.py <path to .cell>`.

Product signals only: turns.jsonl (sealed_at_ms, parent chain) and the receipt
each turn names (call started/ended in ms). Prints a list, oldest turn first:
  {idx, id, sealed_ms, last_call_end_ms, seal_ms, calls, trigger, fence}
seal_ms = sealed_at_ms - the last call's end, the time the product took to seal
after the tool call finished (null when the turn has no call).
"""
import json
import os
import sys


def turns(cell):
    with open(os.path.join(cell, "turns.jsonl")) as f:
        return [json.loads(l) for l in f if l.strip()]


def receipt(cell, rid):
    try:
        with open(os.path.join(cell, "receipts", rid + ".json")) as f:
            return json.load(f)
    except FileNotFoundError:
        return {}


def rows(cell):
    out = []
    for i, t in enumerate(turns(cell)):
        calls = receipt(cell, t.get("receipt", "")).get("calls", [])
        end = max((c.get("ended", 0) for c in calls), default=None)
        out.append({"idx": i, "id": t["id"], "sealed_ms": t["sealed_at_ms"],
                    "last_call_end_ms": end,
                    "seal_ms": t["sealed_at_ms"] - end if end else None,
                    "calls": len(calls), "trigger": t.get("trigger"),
                    "fence": t.get("fence")})
    return out


if __name__ == "__main__":
    print(json.dumps(rows(sys.argv[1])))
