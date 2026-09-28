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
import hashlib
import json
import os
import pathlib
import sys

RECORD = "record.jsonl"
MANIFEST = "artifacts.sha256"

# The fields every final row carries, whatever the caller supplied. A field
# the run genuinely does not know is written as null — an absence is a fact,
# not a gap to be smoothed over.
FINAL_FIELDS = [
    "task", "arm", "model", "variant", "seed", "base_commit", "reference_commit",
    "image", "platform", "emulated", "rig_rev", "bin_sha256_16",
    "patch_source", "patch_bytes", "exit_code", "ended", "agent_seconds",
    "cost_usd_harness", "cost_usd_guard", "prompt_tokens", "completion_tokens",
    "calls", "score", "pass", "flagged", "flag_reasons", "grade_status",
    "judge_model", "judge_prompt_version", "judge_tokens", "failed_blockers",
    "criteria_pass", "criteria_total", "apply_failed", "notes",
]


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


def finalize(args):
    given = json.loads(args.json)
    final = {field: given.get(field) for field in FINAL_FIELDS}
    final["event"] = "final"
    append(args.run_dir, final)
    run_dir = pathlib.Path(args.run_dir)
    manifest = []
    for path in sorted(run_dir.rglob("*")):
        if path.is_dir() or path.name in (MANIFEST,) or path.name == RECORD:
            continue
        # Keys never enter the manifest's digest of themselves; the record says
        # they were scrubbed instead.
        if path.name == "config.json":
            manifest.append(f"{hashlib.sha256(b'<scrubbed-key-record>').hexdigest()}  {path.relative_to(run_dir)} (key scrubbed)")
            continue
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        manifest.append(f"{digest}  {path.relative_to(run_dir)}")
    (run_dir / MANIFEST).write_text("\n".join(manifest) + "\n")
    (run_dir / "DONE").write_text(final["event"] + "\n")
    return 0


def main():
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="cmd", required=True)
    for name in ("start", "row", "finalize"):
        p = sub.add_parser(name)
        p.add_argument("--run-dir", required=True)
        p.add_argument("--json", default="{}")
    args = parser.parse_args()
    return {"start": start, "row": row, "finalize": finalize}[args.cmd](args)


if __name__ == "__main__":
    sys.exit(main())