#!/usr/bin/env python3
"""Calibrate a preregistered holdout on Spark without making model requests."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import time
import traceback
import xml.etree.ElementTree as ET


def call(args, **kwargs):
    return subprocess.run(args, check=True, text=True, capture_output=True, **kwargs).stdout


def write(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")


def calibrate(task, root):
    key = task["id"]
    out = root / "prepared" / key
    out.mkdir(parents=True, exist_ok=False)
    name = "afholdout-prepare-" + key
    receipt = {"task": key, "status": "infrastructure_rejected", "paid_calls": 0}
    try:
        assert task["metadata_ready"], "No linked issue or upstream test patch"
        image = json.loads(call(["docker", "image", "inspect", task["image"]]))[0]
        repo = image["Config"]["WorkingDir"]
        assert repo.startswith("/") and repo != "/", "Missing repository working directory"
        call(["docker", "run", "-d", "--name", name, "--platform", "linux/amd64",
              "--cpus", "2", "--memory", "8g", "--entrypoint", "/bin/bash",
              image["Id"], "-c", "sleep 7200"])

        def inside(args, timeout=120):
            return call(["docker", "exec", "-w", repo, name] + args, timeout=timeout)

        base = inside(["git", "rev-parse", "HEAD"]).strip()
        assert base == task["base_commit"], "Cached image is not the frozen issue base"
        assert not inside(["git", "status", "--porcelain", "--untracked-files=no"]).strip()
        inside(["git", "archive", "--format=tar", "-o", "/tmp/holdout-base.tar", "HEAD"])
        call(["docker", "cp", name + ":/tmp/holdout-base.tar", str(out / "base-source.tar")])
        # Only shared test tooling is installed. No task-specific dependency fixes
        # or acceptance exclusions may turn a rejected fixture into an easy case.
        if subprocess.run(["docker", "exec", name, "python", "-c", "import pytest_timeout"],
                          capture_output=True).returncode:
            (out / "install-timeout.log").write_text(inside(
                ["python", "-m", "pip", "install", "pytest-timeout==2.4.0"], timeout=180))
        runtime = call(["docker", "commit", name]).strip()
        call(["docker", "network", "disconnect", "bridge", name])
        targets = [p for p in task["test_files"] if Path(p).name != "conftest.py"]
        assert targets, "No executable upstream test target"
        command = ["python", "-m", "pytest", "-q", "-o", "addopts=", "--timeout=30",
                   "--disable-warnings"] + targets
        roots = set()
        for target in task["test_files"]:
            bits = Path(target).parts
            position = next((i for i, part in enumerate(bits) if part in ("tests", "test")), None)
            roots.add(str(Path(*bits[:position + 1])) if position is not None else target)
        metadata = {**task, "repo_dir": repo, "image_id": image["Id"],
                    "runtime_image_id": runtime, "base_commit": base,
                    "test_roots": sorted(roots), "test_command": command,
                    "platform": "linux/amd64", "agent_platform": "linux/arm64"}
        for patch in ("heldout", "solution"):
            call(["docker", "cp", str(root / "private" / key / (patch + ".patch")),
                  name + ":/tmp/" + patch + ".patch"])
        for phase in ("base", "gold"):
            inside(["git", "reset", "--hard", base])
            inside(["git", "clean", "-fd"])
            if phase == "gold":
                inside(["git", "apply", "/tmp/solution.patch"])
            inside(["git", "apply", "/tmp/heldout.patch"])
            started = time.time()
            with (out / (phase + ".log")).open("w") as log:
                result = subprocess.run(["docker", "exec", "-w", repo, name] + command +
                                        ["--junitxml=/tmp/" + phase + ".xml"],
                                        stdout=log, stderr=subprocess.STDOUT, timeout=900)
            call(["docker", "cp", name + ":/tmp/" + phase + ".xml", str(out / (phase + ".xml"))])
            cases = list(ET.parse(out / (phase + ".xml")).iter("testcase"))
            metadata[phase] = {"exit": result.returncode, "seconds": round(time.time() - started, 2),
                               "tests": len(cases),
                               "failures": sum(t.find("failure") is not None for t in cases),
                               "errors": sum(t.find("error") is not None for t in cases),
                               "skipped": sum(t.find("skipped") is not None for t in cases)}
            write(out / "calibration.json", metadata)
        assert metadata["base"]["failures"] > 0 and metadata["base"]["errors"] == 0, "Base must fail assertions, not environment setup"
        assert metadata["gold"]["exit"] == 0 and metadata["gold"]["tests"] > metadata["gold"]["skipped"], "Reference must pass executable tests"
        assert metadata["gold"]["tests"] == metadata["base"]["tests"], "Reference changed test collection"
        write(out / "ready.json", metadata)
        receipt.update(status="ready", image_id=image["Id"], runtime_image_id=runtime)
    except Exception:
        receipt["error"] = traceback.format_exc()
    finally:
        subprocess.run(["docker", "rm", "-f", name], capture_output=True)
        write(out / "receipt.json", receipt)
    print(json.dumps(receipt), flush=True)
    return receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, required=True)
    args = parser.parse_args()
    root = args.root
    plan = json.loads((root / "selection.json").read_text())
    fixtures = json.loads((root / "fixture-hashes.json").read_text())
    for name, digest in fixtures.items():
        assert hashlib.sha256((root / name).read_bytes()).hexdigest() == digest, name
    with (root / "PREPARATION-RESERVED").open("x") as reservation:
        reservation.write(str(time.time()) + "\n")
    rows = {r["id"]: r for r in json.loads((root / "metadata.json").read_text())}
    accepted, receipts, repositories = [], [], set()
    for selected in plan["primary"] + plan["reserves"]:
        task = rows[selected["id"]]
        if task["repo"] in repositories:
            continue
        receipt = calibrate(task, root)
        receipts.append(receipt)
        if receipt["status"] == "ready":
            accepted.append(task)
            repositories.add(task["repo"])
        write(root / "PREPARATION.json", {"accepted": accepted, "receipts": receipts, "paid_calls": 0})
        if len(accepted) == 5:
            break
    write(root / "PREPARATION-COMPLETE.json", {"accepted": accepted, "receipts": receipts,
          "paid_calls": 0, "status": "ready" if len(accepted) == 5 else "needs_review"})
    return 0 if len(accepted) == 5 else 2


if __name__ == "__main__":
    raise SystemExit(main())
