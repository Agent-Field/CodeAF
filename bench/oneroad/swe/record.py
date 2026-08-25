#!/usr/bin/env python3
"""record.py — one SWE cell's meta.json, from the container's own leavings."""
import json, os, subprocess, sys

CELL, ARM, TASK, SEED, WALL, CODE, VWALL, MODEL, TASKDIR = sys.argv[1:10]
ONEROAD = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(ONEROAD, "lib"))

V = os.path.join(CELL, "logs", "verifier")


def load(name, default=None):
    try:
        return json.load(open(os.path.join(V, name)))
    except (OSError, ValueError):
        return default


verifier = load("verifier_results.json")
reward = load("reward.json")
details = load("reward_details.json")
stages = load("oneroad_stages.json", {})

# THE VERDICT, AND ITS HONEST ABSENCE.
#
# A validation-primary task has no valid reward without the validation agent,
# and run_aggregate.py says so itself rather than falling back to verifier-only
# scoring. That refusal is recorded as unavailable — never as a zero, which
# would read as "the attempt failed" about a stage that never ran.
validation_primary = bool(stages.get("validation_primary"))
passed = failed = None
if isinstance(verifier, dict):
    passed = verifier.get("tests_passed", verifier.get("passed"))
    failed = verifier.get("tests_failed", verifier.get("failed"))
reward_value = (reward or {}).get("reward") if isinstance(reward, dict) else None
if validation_primary:
    verdict = "unavailable(validation-primary, no judge keys)"
elif reward_value is None:
    verdict = "unavailable(no reward produced)"
else:
    verdict = "pass" if float(reward_value) >= 1.0 else "fail"

patch_path = os.path.join(V, "agent.patch")
patch_bytes = os.path.getsize(patch_path) if os.path.exists(patch_path) else 0
changed = 0
if patch_bytes:
    changed = sum(1 for l in open(patch_path, errors="replace")
                  if l.startswith("+++ ") and not l.startswith("+++ /dev/null"))

meta = {
    "track": "senior-swe-bench", "task": TASK, "harness": ARM, "seed": SEED,
    "model": MODEL, "wall_s": int(WALL), "verify_wall_s": int(VWALL),
    "exit": int(CODE),
    "outcome": open(os.path.join(CELL, "outcome")).read().strip()
        if os.path.exists(os.path.join(CELL, "outcome")) else "UNKNOWN",
    "verdict": verdict, "reward": reward_value,
    "tests_passed": passed, "tests_failed": failed,
    "changed_files": changed, "agent_patch_bytes": patch_bytes,
    "validation_primary": validation_primary,
    "verifier_stages_ran": stages.get("ran", []),
    "verifier_stages_skipped": stages.get("skipped", []),
    "loadavg_before": open(os.path.join(CELL, "loadavg-before")).read().strip(),
    "loadavg_after": open(os.path.join(CELL, "loadavg-after")).read().strip(),
}

# The road columns, from the profile the container wrote through its bind mount.
if ARM.startswith("aforge"):
    sessions = []
    root = os.path.join(CELL, "profile", "v3", "projects")
    for base, dirs, _ in os.walk(root):
        if os.path.basename(os.path.dirname(base)) == "projects":
            sessions.append(base)
    if sessions:
        out = subprocess.run(
            [sys.executable, os.path.join(ONEROAD, "lib", "road.py"), sessions[0],
             "--timeline", os.path.join(CELL, "timeline.json")],
            capture_output=True, text=True)
        if out.stdout.strip():
            meta.update(json.loads(out.stdout))
    else:
        meta["road"] = "unreadable(no session folder)"
else:
    meta.update({"road": "n/a", "route": "n/a", "armed": "n/a", "parts": 0,
                 "peak_workers": "n/a", "refused": "n/a",
                 "cost_usd": "", "cost_source": "not-self-reported"})

json.dump(meta, open(os.path.join(CELL, "meta.json"), "w"), indent=2)
print(json.dumps({k: meta[k] for k in
                  ("task", "harness", "verdict", "reward", "road", "wall_s", "changed_files")
                  if k in meta}))
