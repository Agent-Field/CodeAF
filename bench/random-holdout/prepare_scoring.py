"""Reassess preserved controls and overlay the frozen Pi runtime on Spark."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys

from prepare import call, validate_controls, write

TOOLCHAIN = "sha256:8df2618cf7fba2bf82e79bb235d6b50bb9b4857bc7753b0f94017db25f004b4d"


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def reassess(root, out):
    assert (root / "PREPARATION-COMPLETE.json").exists(), "Original preparation is still running"
    for name, expected in json.loads((root / "fixture-hashes.json").read_text()).items():
        assert digest(root / name) == expected, name
    selection = json.loads((root / "selection.json").read_text())
    tasks = {t["id"]: t for t in json.loads((root / "metadata.json").read_text())}
    accepted, evidence, repositories = [], [], set()
    for item in selection["primary"] + selection["reserves"]:
        task = tasks[item["id"]]
        if task["repo"] in repositories:
            continue
        original = root / "prepared" / task["id"]
        metadata = json.loads((original / "calibration.json").read_text())
        record = {"task": task["id"], "source": str(original),
                  "hashes": {n: digest(original / n) for n in
                             ("calibration.json", "base.xml", "gold.xml", "base-source.tar", "receipt.json")}}
        try:
            validate_controls(metadata)
        except AssertionError as error:
            record.update(status="rejected", reason=str(error))
        else:
            # Preserve old receipts, tests, source and runtime. Reclassification
            # changes no model result and never reruns a reference for a green draw.
            destination = out / "prepared" / task["id"]
            destination.mkdir(parents=True)
            for name in ("base-source.tar", "gold.xml", "base.xml"):
                (destination / name).symlink_to(original / name)
            write(destination / "ready.json", metadata)
            record["status"] = "ready"
            accepted.append(task)
            repositories.add(task["repo"])
        evidence.append(record)
        if len(accepted) == 5:
            break
    receipt = {"policy": "source failure then unchanged reference tests pass",
               "policy_sha256": digest(Path(__file__).with_name("prepare.py")),
               "selection_sha256": digest(root / "selection.json"),
               "accepted": accepted, "evidence": evidence, "paid_calls": 0}
    write(out / "REASSESSMENT.json", receipt)
    assert len(accepted) == 5, "Fewer than five distinct calibrated repositories"
    return accepted


def prepare_pi(out, tasks):
    source = "afholdout-toolchain-" + hashlib.sha256(str(out).encode()).hexdigest()[:12]
    toolchain = out / "pi-toolchain"
    toolchain.mkdir()
    call(["docker", "create", "--name", source, "--platform", "linux/amd64", TOOLCHAIN])
    try:
        call(["docker", "cp", source + ":/usr/bin/node", str(toolchain / "node")])
        call(["docker", "cp", source + ":/usr/lib/node_modules", str(toolchain / "node_modules")])
    finally:
        subprocess.run(["docker", "rm", "-f", source], capture_output=True)
    records = []
    for task in tasks:
        ready = json.loads((out / "prepared" / task["id"] / "ready.json").read_text())
        name = source + "-" + task["id"]
        call(["docker", "run", "-d", "--name", name, "--platform", "linux/amd64",
              "--network", "none", "--cpus", "2", "--memory", "8g",
              "--entrypoint", "/bin/bash", ready["runtime_image_id"], "-c", "sleep 1800"])
        try:
            call(["docker", "exec", name, "mkdir", "-p", "/usr/bin", "/usr/lib"])
            call(["docker", "cp", str(toolchain / "node"), name + ":/usr/bin/node"])
            call(["docker", "cp", str(toolchain / "node_modules"), name + ":/usr/lib/node_modules"])
            call(["docker", "exec", name, "ln", "-sf",
                  "/usr/lib/node_modules/@earendil-works/pi-coding-agent/dist/cli.js", "/usr/bin/pi"])
            node = call(["docker", "exec", name, "node", "--version"], timeout=90).strip()
            pi = call(["docker", "exec", name, "pi", "--version"], timeout=90).strip()
            assert node == "v24.12.0" and pi == "0.84.2", (node, pi)
            record = {"task": task["id"], "base_runtime_image_id": ready["runtime_image_id"],
                      "pi_runtime_image_id": call(["docker", "commit", name]).strip(),
                      "toolchain_image_id": TOOLCHAIN, "node_version": node, "pi_version": pi}
            records.append(record)
            write(out / "pi-runtimes.json", records)
        finally:
            subprocess.run(["docker", "rm", "-f", name], capture_output=True)
    return records


def main():
    root, out = (Path(arg).resolve() for arg in sys.argv[1:])
    out.mkdir(exist_ok=False)
    tasks = reassess(root, out)
    runtimes = prepare_pi(out, tasks)
    write(out / "SCORING-PREPARATION.json", {"accepted": tasks, "pi_runtimes": runtimes,
          "paid_calls": 0, "status": "ready_for_actual_client_preflight"})


if __name__ == "__main__":
    main()
