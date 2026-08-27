#!/usr/bin/env python3
"""record.py — one SWE cell's meta.json, from the container's own leavings."""
import glob, json, os, subprocess, sys

CELL, ARM, TASK, SEED, WALL, CODE, VWALL, MODEL, TASKDIR = sys.argv[1:10]
SETTLE_REASON = sys.argv[10] if len(sys.argv) > 10 else "n/a"
# The file is the source of truth; the argument is only a convenience. Reading it
# here as well means a settle_reason written by the loop cannot be lost to a
# quoting accident in the caller.
if SETTLE_REASON in ("", "n/a"):
    try:
        SETTLE_REASON = open(os.path.join(CELL, "settle_reason")).read().strip() or "n/a"
    except OSError:
        SETTLE_REASON = "n/a"
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
    "outcome": "PLACEHOLDER",
    "settle_reason": SETTLE_REASON,
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
    # THE SESSION IS TWO LEVELS UNDER projects/, NOT ONE. The walk here tested
    # `dirname(base) == "projects"`, which matches the PROJECT directory — the
    # one named after the workspace path — and handed road.py a directory with
    # no transcript.jsonl in it. Every SWE cell therefore recorded
    # `road=unreadable`. The layout is projects/<project>/<session>/, so it is
    # globbed at exactly that depth, the same way every other reader here does it.
    sessions = sorted(
        d for d in glob.glob(os.path.join(CELL, "profile", "v3", "projects", "*", "*"))
        if os.path.isdir(d) and os.path.exists(os.path.join(d, "transcript.jsonl")))
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

# ── the wall is not a verdict ───────────────────────────────────────────────
#
# A cell that hit the wall was recorded DNF, and DNF reads as "did not finish".
# On prefect that hid a PASS: the work landed, the dataset's own verifier scored
# reward 1.0, and only the runner's clock ran out afterwards. Calling that "did
# not finish" is the runner's limitation masquerading as the attempt's result.
#
# So the wall is recorded as a fact about the CLOCK and the verdict is left to
# the evidence: work that landed and verified is OK(wall); a wall reached with
# nothing landed is still DNF, because there is nothing to have finished.
raw_outcome = "UNKNOWN"
outcome_path = os.path.join(CELL, "outcome")
if os.path.exists(outcome_path):
    raw_outcome = open(outcome_path).read().strip()

landed_work = bool(patch_bytes) or bool(changed)
verified = reward_value is not None and float(reward_value) >= 1.0
if raw_outcome == "DNF":
    if verified or landed_work:
        meta["outcome"] = "OK(wall)"
        meta["settle_reason"] = (
            "wall reached, but work landed%s — the clock ran out, the attempt did not"
            % (" and the verifier passed it" if verified else ""))
    else:
        meta["outcome"] = "DNF"
        meta["settle_reason"] = "wall reached with nothing landed"
else:
    meta["outcome"] = raw_outcome

json.dump(meta, open(os.path.join(CELL, "meta.json"), "w"), indent=2)
print(json.dumps({k: meta[k] for k in
                  ("task", "harness", "verdict", "reward", "road", "wall_s", "changed_files")
                  if k in meta}))
