#!/usr/bin/env python3
"""Write the run's final record row: the combined grade, the scan, the cost
readings and the run's own facts, into record.jsonl's final row, then the
DONE marker and the sha256 manifest. The one place the contract fields are
assembled — so a field the rig never fills is visible here, as null."""

import argparse
import json
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).parent))
import rubric  # noqa: E402  (the status vocabulary)

COST_CROSSCHECK_DELTA = 0.05  # dollars; beyond this the two readings disagree


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--run-dir", required=True)
    parser.add_argument("--task-dir", required=True)
    args = parser.parse_args()
    run_dir = pathlib.Path(args.run_dir)
    meta = json.loads((run_dir / "meta.json").read_text()) if (run_dir / "meta.json").exists() else {}
    cost = json.loads((run_dir / "cost.json").read_text()) if (run_dir / "cost.json").exists() else {}
    grade_path = run_dir / "grade.json"
    scan_path = run_dir / "scan.json"
    grade = json.loads(grade_path.read_text()) if grade_path.exists() else {}
    scan = json.loads(scan_path.read_text()) if scan_path.exists() else {}

    grade_status = grade.get("status", "rig")
    score = grade.get("score")
    flagged = scan.get("flagged")
    # THE FLAG IS THE GRADE'S OVERRIDE, NOT A NOTE. A flagged run scores 0 and
    # counts toward flag rate, whatever the rubric said.
    if flagged:
        score = 0.0
        grade_status = "done"

    harness_cost = cost.get("cost_usd")
    guard_cost = cost.get("cost_usd_guard")
    notes = []
    if grade.get("failed_blockers"):
        notes.append("failed blockers: " + ", ".join(grade["failed_blockers"]))
    if harness_cost is not None and guard_cost is not None:
        if abs(harness_cost - guard_cost) > COST_CROSSCHECK_DELTA:
            notes.append(f"cost readings disagree: harness ${harness_cost:.3f} vs guard ${guard_cost:.3f}")

    criteria = grade.get("criteria", {})
    final = {
        **{k: meta.get(k) for k in (
            "task", "arm", "model", "variant", "seed", "base_commit", "reference_commit",
            "image", "platform", "emulated", "rig_rev", "bin_sha256_16")},
        "patch_source": meta.get("patch_source"),
        "patch_bytes": meta.get("patch_bytes"),
        "exit_code": meta.get("exit_code"),
        "ended": meta.get("ended"),
        "agent_seconds": meta.get("agent_seconds"),
        "cost_usd_harness": harness_cost,
        "cost_usd_guard": guard_cost,
        "prompt_tokens": cost.get("prompt_tokens"),
        "completion_tokens": cost.get("completion_tokens"),
        "calls": cost.get("calls"),
        "score": score,
        "pass": bool(score) and score > 0 and grade.get("status") == "done",
        "flagged": flagged,
        "flag_reasons": scan.get("reasons", []),
        "grade_status": grade_status,
        "judge_model": grade.get("judge_model"),
        "judge_prompt_version": grade.get("judge_prompt_version"),
        "judge_tokens": grade.get("judge_tokens"),
        "failed_blockers": grade.get("failed_blockers", []),
        "criteria_pass": sum(1 for e in criteria.values() if e.get("status") == rubric.PASS),
        "criteria_total": len(criteria),
        "apply_failed": bool((run_dir / "logs" / "grade" / "apply.txt").exists()),
        "notes": "; ".join(notes),
    }
    import record
    record.append(run_dir, {**final, "event": "final"})
    (run_dir / "DONE").write_text("final\n")
    print(json.dumps(final, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())