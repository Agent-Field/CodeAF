#!/usr/bin/env python3
"""The run record: an append-only JSONL per run directory, plus the DONE
marker and the sha256 manifest the minimal rig's attempt contract teaches.

Every row carries the fields the run knows at the time it is appended; the
final row carries the full contract the rig reports on: task, arm, model,
variant, seed, base commit, image, platform, patch source and bytes, exit and
ending, wall seconds, cost (harness-reported AND guard-metered — two readings,
never one), tokens, the score with its failed blockers, the egress flag with
its reasons, the judge's model and prompt version, and the rig revision that
produced the row. A missing grade appends `score: null` with status `rig` —
never a 0, never an omission.

    record.py start --run-dir D --json '{"...": ...}'
    record.py row   --run-dir D --json '{"...": ...}'
    record.py finalize --run-dir D --json '{"...": ...}'
"""

import argparse
import datetime
import json
import pathlib
import sys

RECORD = "record.jsonl"


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat(timespec="seconds")


def append(run_dir, row):
    row = dict(row)
    row.setdefault("ts", now())
    path = pathlib.Path(run_dir) / RECORD
    with path.open("a") as fh:
        fh.write(json.dumps(row, sort_keys=False) + "\n")
    return row


def start(args):
    append(args.run_dir, dict(json.loads(args.json), event="start"))
    return 0


def row(args):
    append(args.run_dir, dict(json.loads(args.json)))
    return 0


def main():
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="cmd", required=True)
    for name in ("start", "row"):
        p = sub.add_parser(name)
        p.add_argument("--run-dir", required=True)
        p.add_argument("--json", default="{}")
    args = parser.parse_args()
    return {"start": start, "row": row}[args.cmd](args)


if __name__ == "__main__":
    sys.exit(main())