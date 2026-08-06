#!/usr/bin/env python3
"""Recompute the `routing` summary of existing rows from their stored events.

The raw events are captured in every row, so a fix to how they are summarised
does not need the cells re-run — which matters, because re-running them would
cost real money to answer a question the data already contains.

Used once, after `summarize_events` was corrected to count an attempt above
rung 0 as an escalation. The cascade path records what it had already tried in
`escalation`; the leaf path does not, so keying on that field alone missed
every leaf escalation, and leaf escalations are the only kind these tasks
produce.

Usage: python3 reroute.py results-armB.jsonl [...]
"""
import json
import sys

from collect import summarize_events


def main():
    for path in sys.argv[1:]:
        rows = []
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                row = json.loads(line)
                row["routing"] = summarize_events((row.get("ledger") or {}).get("events") or [])
                rows.append(row)
        with open(path, "w") as f:
            for row in rows:
                f.write(json.dumps(row) + "\n")
        escalated = sum((r.get("routing") or {}).get("escalated_calls", 0) for r in rows)
        print(f"{path}: {len(rows)} rows, {escalated} escalated calls")


if __name__ == "__main__":
    main()
