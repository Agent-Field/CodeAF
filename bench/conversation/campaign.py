#!/usr/bin/env python3
"""Freeze and run randomized complete blocks; keep every planned attempt visible.

Planning is offline. Execution is explicit, sequential, and refuses changed inputs.
Each arm gets fresh state through run.sh; model calls remain behind its allowlist.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import random
import shutil
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parent
MODEL = "deepseek/deepseek-v4-flash-0731"
# The calibration battery: the six comparable slices, and what a campaign plans
# when nobody says otherwise.
CALIBRATION_SCENARIOS = {"data-tally", "research-brief", "writing-memo", "code-fix",
                         "followup-while-working", "revision-midwork"}
# Selectable by name, and deliberately not part of that default. A scenario that
# joined the battery silently would change what every later campaign measured
# and would make its numbers incomparable with the ones already collected;
# --scenarios multi-defect-pipeline is a separate experiment, planned on purpose.
EXTRA_SCENARIOS = {"multi-defect-pipeline"}
SCENARIOS = CALIBRATION_SCENARIOS | EXTRA_SCENARIOS


def sha(path):
    with open(path, "rb") as handle:
        return hashlib.file_digest(handle, "sha256").hexdigest()


def inputs():
    # Hash the rig and fixtures themselves, so a changed judge starts an experiment.
    return {str(p.relative_to(ROOT)): sha(p) for p in sorted(ROOT.rglob("*"))
            if p.is_file() and p.suffix in {".sh", ".py"} and "test" not in p.parts}


def executable_identity(path):
    """A JS entry point is not its implementation; freeze its package too."""
    path = Path(path)
    identity = {"entry_sha256": sha(path)}
    if path.suffix not in {".js", ".mjs", ".cjs"}:
        return identity
    package = next((parent for parent in path.parents if (parent / "package.json").is_file()), None)
    if package is None:
        raise ValueError("script entry has no package identity: " + str(path))
    files = {}
    # Installed package code, prompts and data are part of the harness. Nested
    # dependencies are not claimed frozen; record that boundary explicitly.
    for directory, dirs, names in os.walk(package):
        dirs[:] = sorted(d for d in dirs if d not in {"node_modules", ".git"})
        for name in sorted(names):
            file = Path(directory) / name
            if file.is_file():
                files[str(file.relative_to(package))] = sha(file)
    identity.update(package_root=str(package), package_file_count=len(files),
                    package_sha256=hashlib.sha256(json.dumps(files, sort_keys=True).encode()).hexdigest(),
                    boundary="installed package; external runtime and nested dependencies not frozen")
    return identity


def plan(args):
    arms = args.arms.split(",")
    scenarios = args.scenarios.split(",")
    if len(set(arms)) != len(arms) or not set(arms) <= {"aforge", "pi", "omp"}:
        raise ValueError("arms must be distinct members of aforge,pi,omp")
    if len(arms) < 2 or not scenarios or not set(scenarios) <= SCENARIOS:
        raise ValueError("choose at least two arms and supported comparable scenarios")
    if len(set(scenarios)) != len(scenarios) or args.repeats < 1 or args.cap < 1:
        raise ValueError("scenarios must be distinct; repeats and cap must be positive")
    binaries = {}
    for arm in arms:
        path = (args.aforge if arm == "aforge" else shutil.which(arm))
        if not path or not Path(path).is_file():
            raise ValueError("binary missing: " + arm)
        path = str(Path(path).resolve())
        binaries[arm] = {"path": path, "sha256": sha(path), "identity": executable_identity(path)}
    rng = random.Random(args.seed)
    schedule = []
    for repetition in range(args.repeats):
        order = scenarios.copy()
        rng.shuffle(order)
        for scenario in order:
            arm_order = arms.copy()
            rng.shuffle(arm_order)
            for arm in arm_order:
                schedule.append({"block_id": str(repetition + 1), "scenario": scenario,
                                 "arm": arm, "ordinal": len(schedule) + 1})
    manifest = {"schema": 1, "experiment_id": args.id, "seed": args.seed,
                "purpose": "calibration" if args.repeats < 5 else "comparison",
                "expected_arms": arms, "repeats": args.repeats, "model_pin": MODEL,
                "effort": "low", "cap_s": args.cap, "slow_seconds": 60,
                "binaries": binaries, "rig_inputs": inputs(), "schedule": schedule,
                "maximum_cell_seconds": len(schedule) * args.cap,
                "quality_rule": "verdict pass AND every assertion pass",
                "machine": {"system": platform.system(), "release": platform.release(),
                            "architecture": platform.machine(), "cpu_count": os.cpu_count()},
                "condition_id": "clean-profile-v2;fresh-state;slow60;no-shared-live-load"}
    with open(args.manifest, "x") as handle:
        json.dump(manifest, handle, indent=2)
    print("Frozen %d cells; maximum cell time %.1f minutes; no model called."
          % (len(schedule), manifest["maximum_cell_seconds"] / 60))


def execute(args):
    manifest_path = Path(args.manifest).resolve()
    manifest = json.loads(manifest_path.read_text())
    if inputs() != manifest["rig_inputs"]:
        raise ValueError("rig or fixture changed since planning; create a new manifest")
    for arm, binary in manifest["binaries"].items():
        if sha(binary["path"]) != binary["sha256"]:
            raise ValueError(arm + " binary changed since planning")
        if binary.get("identity") and executable_identity(binary["path"]) != binary["identity"]:
            raise ValueError(arm + " package changed since planning")
    out = Path(args.out).resolve()
    out.mkdir(parents=True, exist_ok=True)
    receipt = out / "manifest.json"
    if receipt.exists() and receipt.read_bytes() != manifest_path.read_bytes():
        raise ValueError("output belongs to a different manifest")
    if not receipt.exists():
        receipt.write_bytes(manifest_path.read_bytes())
    # A completed cell is immutable. A partial one needs adjudication, not a retry
    # that silently drops its original latency, cost, or failure.
    rows = []
    for cell in manifest["schedule"]:
        if (out / "STOP").exists():
            print("Stop requested; completed evidence retained, remaining attempts not run.")
            return
        cell_out = out / ("%03d-%s-%s" % (cell["ordinal"], cell["scenario"], cell["arm"]))
        finished = cell_out / "campaign-row.json"
        if finished.exists():
            rows.append(json.loads(finished.read_text()))
            continue
        if cell_out.exists():
            raise ValueError("partial cell requires adjudication: " + str(cell_out))
        cell_out.mkdir()
        # Ambient benchmark overrides must not change a frozen experiment.
        env = {k: v for k, v in os.environ.items() if not k.startswith("CONV_")}
        for arm, binary in manifest["binaries"].items():
            env[arm.upper() + "_BIN"] = binary["path"]
        env.update(CONV_SLOW_SECONDS=str(manifest["slow_seconds"]),
                   CONV_CSV=str(out / "raw.csv"))
        command = [str(ROOT / "run.sh"), "--arms", cell["arm"], "--scenarios", cell["scenario"],
                   "--cap", str(manifest["cap_s"]), "--model", manifest["model_pin"],
                   "--effort", manifest["effort"], "--out", str(cell_out / "evidence"), "--keep"]
        print("%d/%d %s %s" % (cell["ordinal"], len(manifest["schedule"]),
                               cell["scenario"], cell["arm"]), flush=True)
        start = time.monotonic()
        with open(cell_out / "runner.log", "w") as log:
            result = subprocess.run(command, env=env, stdout=log, stderr=subprocess.STDOUT)
        elapsed = time.monotonic() - start
        raw = cell_out / "evidence/results.jsonl"
        parsed = [json.loads(line) for line in raw.read_text().splitlines() if line.strip()] if raw.exists() else []
        if len(parsed) == 1:
            row = parsed[0]
        else:
            row = dict(cell, verdict="skipped", comparable="no", checks=[], cost_usd=None,
                       wall_s=None, reason="runner did not produce exactly one result", model_pin=MODEL)
        row.update(experiment_id=manifest["experiment_id"], block_id=cell["block_id"],
                   expected_arms=manifest["expected_arms"], condition_id=manifest["condition_id"],
                   runner_wall_s=elapsed, runner_exit=result.returncode,
                   arm_binary_sha256=manifest["binaries"][cell["arm"]]["sha256"],
                   evidence=str(cell_out / "evidence"))
        door_path = cell_out / "evidence" / (cell["scenario"] + "-" + cell["arm"]) / "door.json"
        if door_path.exists():
            door = json.loads(door_path.read_text())
            row["interaction_witness"] = door
            try:
                row["followup_response_s"] = float(door["answer_first_seen_at"]) - float(door["midwork_sent_at"])
            except (KeyError, ValueError, TypeError):
                row["followup_response_s"] = None
        finished.write_text(json.dumps(row, sort_keys=True) + "\n")
        rows.append(row)
        (out / "results.jsonl").write_text("".join(json.dumps(r) + "\n" for r in rows))
        print("  %s; %ss; cost=%s" % (row["verdict"], row.get("wall_s"), row.get("cost_usd")), flush=True)
        if row["verdict"] in {"unsupported", "skipped"}:
            raise ValueError("infrastructure failure; campaign stopped with attempt preserved")
    (out / "results.jsonl").write_text("".join(json.dumps(r) + "\n" for r in rows))
    print("All planned cells recorded; analysis must still check comparability and sample size.")


def build_parser():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    p = sub.add_parser("plan")
    p.add_argument("manifest")
    p.add_argument("--id", required=True)
    p.add_argument("--aforge", default=str(ROOT.parents[1] / "bin/aforge"))
    p.add_argument("--arms", default="aforge,pi,omp")
    p.add_argument("--scenarios", default=",".join(sorted(CALIBRATION_SCENARIOS)))
    p.add_argument("--repeats", type=int, default=2)
    p.add_argument("--seed", type=int, default=20260905)
    p.add_argument("--cap", type=int, default=240)
    p = sub.add_parser("run")
    p.add_argument("manifest")
    p.add_argument("--out", required=True)
    return parser


def main():
    args = build_parser().parse_args()
    try:
        (plan if args.command == "plan" else execute)(args)
    except (ValueError, OSError) as error:
        print(str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
