"""Run frozen issue/seed blocks with two concurrent native harness trials."""
import concurrent.futures
import datetime
import hashlib
import json
from pathlib import Path
import subprocess
import sys


def validate_blocks(plan):
    blocks = plan["blocks"]
    flattened = [cell for block in blocks for cell in block]
    if flattened != plan["cells"] or len({tuple(c) for c in flattened}) != len(flattened):
        raise ValueError("Blocks must cover the frozen trials exactly once")
    arms = set(plan["arms"])
    for block in blocks:
        if len(block) != len(arms) or {c[0] for c in block} != arms:
            raise ValueError("Each block must compare every arm exactly once")
        if len({(c[1], c[2]) for c in block}) != 1:
            raise ValueError("A block must use one issue and seed")
    return blocks


def run(root):
    root = Path(root).resolve()
    manifest = root / "scoring-plan.json"
    plan = json.loads(manifest.read_text())
    blocks = validate_blocks(plan)
    if not plan["preflight_passed"]:
        raise ValueError("Actual-client preflight has not passed")
    for name, expected in plan["input_hashes"].items():
        if hashlib.sha256(Path(name).read_bytes()).hexdigest() != expected:
            raise ValueError("Frozen input changed: " + name)
    # The reservation is deliberately exclusive. Resuming by replaying a block
    # would quietly select new model samples after seeing the first outcomes.
    with (root / "RUN-RESERVED").open("x") as receipt:
        json.dump({"started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
                   "plan_sha256": hashlib.sha256(manifest.read_bytes()).hexdigest()}, receipt)
    logs = root / "launch-logs"
    logs.mkdir(exist_ok=False)

    def one(cell):
        arm, task, seed = cell
        stem = f"{arm}-{task}-seed{seed}"
        with (logs / (stem + ".log")).open("x") as log:
            process = subprocess.run([sys.executable, str(Path(__file__).with_name("run_cell.py")),
                                      str(root), arm, task, str(seed)],
                                     stdout=log, stderr=subprocess.STDOUT)
        path = root / arm / "cells" / f"{task}-seed{seed}" / "result.json"
        result = json.loads(path.read_text()) if path.exists() else None
        return {"cell": cell, "adapter_exit": process.returncode, "result": result}

    results = []
    # A block barrier keeps later easy issues from racing ahead of a slow arm.
    # Arm order is rotated in the frozen plan, not adjusted from live outcomes.
    for block in blocks:
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            completed = list(pool.map(one, block))
        results.extend(completed)
        temporary = root / "trial-results.tmp"
        temporary.write_text(json.dumps(results, indent=2) + "\n")
        temporary.replace(root / "trial-results.json")
        bad = [r for r in completed if r["adapter_exit"] != 0 or r["result"] is None
               or r["result"].get("status") == "infrastructure_error"]
        if bad:
            (root / "RUN-NEEDS-REVIEW.json").write_text(json.dumps(bad, indent=2) + "\n")
            raise RuntimeError("Retained infrastructure failure; no automatic retry or next block")
    (root / "RUN-COMPLETE").write_text(datetime.datetime.now(datetime.timezone.utc).isoformat() + "\n")


if __name__ == "__main__":
    run(sys.argv[1])
