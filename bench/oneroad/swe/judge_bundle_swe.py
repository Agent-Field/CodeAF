#!/usr/bin/env python3
"""judge_bundle_swe.py — the blind judge input for a SWE cell.

Same four sections JUDGE.md asks for, sourced from this track's own artifacts:
ISSUE is the Task section alone (issue.txt), PATCH is the dataset's own
agent.patch, TESTS is the verifier summary, REPORT is the harness's last word.

THE ORACLE NEVER APPEARS HERE. solution/oracle.patch, tests/judge/oracle.patch
and task.toml's [metadata].solution are all excluded by construction — this
reads only the cell directory, and the cell directory never contained them.
"""
import glob, json, os, re, sys

PATCH_CAP, REPORT_CAP = 120_000, 20_000
cell = sys.argv[1]
meta = json.load(open(os.path.join(cell, "meta.json")))
V = os.path.join(cell, "logs", "verifier")


def read(path, cap=None):
    try:
        text = open(path, errors="replace").read()
    except OSError:
        return ""
    return text[:cap] + f"\n\n[... truncated at {cap} bytes ...]" if cap and len(text) > cap else text


def report():
    if meta["harness"].startswith("aforge"):
        pieces = []
        for s in glob.glob(os.path.join(cell, "profile/v3/projects/*/*")):
            for line in read(os.path.join(s, "transcript.jsonl")).splitlines():
                try:
                    e = json.loads(line)
                except ValueError:
                    continue
                if e.get("role") == "assistant" and isinstance(e.get("content"), str) and e["content"].strip():
                    pieces.append(e["content"].strip())
            try:
                for n in json.load(open(os.path.join(s, "tasks.json"))).get("nodes", []):
                    if n.get("report"):
                        pieces.append("--- task report ---\n" + n["report"])
            except (OSError, ValueError):
                pass
        return "\n\n".join(pieces)[:REPORT_CAP]
    raw = re.sub(r"\x1b\[[0-9;?]*[A-Za-z]", "", read(os.path.join(cell, "harness.log")))
    return raw[-REPORT_CAP:]


patch = read(os.path.join(V, "agent.patch"), PATCH_CAP).strip() or "no changes"
tests = [f"- verifier verdict: {meta.get('verdict')}",
         f"- reward: {meta.get('reward')}",
         f"- tests passed: {meta.get('tests_passed')}  failed: {meta.get('tests_failed')}",
         f"- files in the agent patch: {meta.get('changed_files')}",
         f"- verifier stages run: {', '.join(meta.get('verifier_stages_ran') or [])}",
         f"- verifier stages SKIPPED (no judge keys): {', '.join(meta.get('verifier_stages_skipped') or [])}"]
if meta.get("validation_primary"):
    tests.append("- NOTE: this task is validation-primary; its dataset reward needs an "
                 "LLM validation agent that was not run, so no pass/fail is claimed.")

out = ["<!-- blind judge input. JUDGE.md v1. The harness is not named here; it is",
       "     in meta.json beside this file. The reference solution was never in",
       "     this directory and is not shown. -->", "",
       "# ISSUE", "", read(os.path.join(cell, "issue.txt")).strip(), "",
       "# PATCH", "", "```diff", patch, "```", "",
       "# TESTS", "", *tests, "",
       "# REPORT", "", report().strip() or "(the attempt said nothing)", ""]
open(os.path.join(cell, "judge_input.md"), "w").write("\n".join(out))
print("judge_input.md:", os.path.join(cell, "judge_input.md"))
