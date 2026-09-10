"""Bind frozen native runners to one holdout trial, without changing their loops."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys

from grading import grade


def load_runner(path):
    # Each arm runs in its own process because the inherited runners configure
    # module globals. Sharing an interpreter would mix workspaces and graders.
    sys.path.insert(0, str(path.parent))
    spec = importlib.util.spec_from_file_location("holdout_runner", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def run(root, arm, task_id, seed):
    root = Path(root).resolve()
    plan = json.loads((root / "scoring-plan.json").read_text())
    if [arm, task_id, seed] not in plan["cells"]:
        raise ValueError("Trial is not in the frozen scoring plan")
    if not plan["preflight_passed"]:
        raise ValueError("Actual-client preflight has not passed")
    # Check external source, binary, grader, prompt and fixture inputs before
    # starting a paid request. The manifest itself is frozen before any scoring.
    for path, expected in plan["input_hashes"].items():
        if hashlib.sha256(Path(path).read_bytes()).hexdigest() != expected:
            raise ValueError("Frozen input changed: " + path)
    config = plan["arms"][arm]
    task = next(t for t in plan["tasks"] if t["id"] == task_id)
    out = root / arm / "cells" / f"{task_id}-seed{seed}"
    if out.exists():
        raise FileExistsError("Never retry or overwrite a scored trial: " + str(out))
    runner = load_runner(Path(config["runner"]))
    runner.ROOT = Path(config["resources"])
    runner.PREPARED = root / "prepared"
    runner.TASKS = plan["tasks"]
    runner.OUT = root / arm
    runner.MODEL = plan["model"]
    runner.WALL = plan["wall_seconds"]
    os.environ["BENCH_SOURCE"] = config["source"]
    export_tree = runner.common.export_tree if arm == "mini" else runner.export_tree
    shared_grade = lambda task, prepared, output: grade(root, task, prepared, output, export_tree)
    watcher = None
    try:
        if arm in ("base", "candidate"):
            runner.BINARY = Path(config["binary"])
            runner.grade = shared_grade
            result = runner.run_cell((task, seed))
        elif arm == "pi":
            runner.grade = shared_grade
            result = runner.run_cell((task, seed), config["runtime_images"])
        elif arm == "mini":
            runner.CAMPAIGN = root
            runner.common.PREPARED = root / "prepared"
            runner.common.grade = shared_grade
            # The native watcher is a frozen copy of the existing collector,
            # scoped to this campaign and stopped by this trial's result file.
            # It must run alongside inference: a post-hoc bill cannot enforce
            # the same known-cost stop used by the other arms.
            with (root / f"mini-watch-{task_id}-seed{seed}.log").open("x") as log:
                watcher = subprocess.Popen([sys.executable, config["usage_watcher"], str(out)],
                                           stdout=log, stderr=subprocess.STDOUT)
                result = runner.run_cell((task, seed))
                if watcher.wait(timeout=15) != 0:
                    raise RuntimeError("Native mini usage collection failed; retain trial for review")
            # Keep native receipts separate from inherited guard accounting.
            # Unknown guard rows overlap native generations and are not added.
            receipt = root / "mini-usage" / (out.name + ".json")
            if not receipt.exists():
                raise RuntimeError("Missing native mini usage receipt; retain trial for review")
            stop_receipt = root / "mini-usage" / (out.name + "-cost-stop.json")
            accounting = {"native_receipt": str(receipt), "cost_stop": stop_receipt.exists(),
                          "status": "cost_limit" if stop_receipt.exists() else result["status"]}
            (out / "accounting.json").write_text(json.dumps(accounting, indent=2) + "\n")
        else:
            raise ValueError("Unsupported arm")
    finally:
        if watcher is not None and watcher.poll() is None:
            watcher.terminate()
            try:
                watcher.wait(timeout=10)
            except subprocess.TimeoutExpired:
                watcher.kill()
                watcher.wait()
    return result


if __name__ == "__main__":
    run(sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4]))
