#!/usr/bin/env python3
"""judge_bundle.py — one cell's blind judge input, ready to hand to a Sonnet.

JUDGE.md's rule 2 is that the judge is BLIND: it is told the issue, the diff, the
test counts and the attempt's own report, and it is never told which harness
produced them. So this writes exactly those four sections and nothing else — no
arm name, no wall clock, no cost, no road. The harness's identity is in
meta.json beside it, where the recorder reads it and the judge does not.

Usage: judge_bundle.py <cell-dir>
Writes <cell-dir>/judge_input.md.
"""
import glob, json, os, re, sys

# A whole diff can be enormous and a judge that was handed 400 KB reads none of
# it. The cap is generous enough that no honest fix reaches it and the truncation
# is SAID, so a low score over a clipped diff is legible rather than mysterious.
PATCH_CAP = 120_000
REPORT_CAP = 20_000


def read(path, cap=None):
    try:
        with open(path, errors="replace") as fh:
            text = fh.read()
    except OSError:
        return ""
    if cap and len(text) > cap:
        return text[:cap] + f"\n\n[... truncated at {cap} bytes ...]"
    return text


def report_of(cell, meta):
    """What the attempt said it did — and nothing about how it was driven.

    An aforge cell's own words are the session transcript's agent turns plus any
    task report on the checkpoint. A peer arm's are its stdout. Both are the
    attempt's account of itself, which is what report_honesty grades.
    """
    if meta.get("harness", "").startswith("aforge"):
        pieces = []
        for session in glob.glob(os.path.join(cell, "store/v3/projects/*/*")):
            for line in read(os.path.join(session, "transcript.jsonl")).splitlines():
                try:
                    entry = json.loads(line)
                except ValueError:
                    continue
                if entry.get("role") == "assistant" and isinstance(entry.get("content"), str):
                    if entry["content"].strip():
                        pieces.append(entry["content"].strip())
            try:
                tasks = json.load(open(os.path.join(session, "tasks.json")))
            except (OSError, ValueError):
                tasks = {}
            for node in tasks.get("nodes", []) or []:
                if node.get("report"):
                    pieces.append("--- task report ---\n" + node["report"])
        return "\n\n".join(pieces)[:REPORT_CAP]
    # A peer arm's stdout carries terminal control sequences; they are stripped so
    # the judge reads words rather than escape codes.
    raw = read(os.path.join(cell, "harness.log"))
    raw = re.sub(r"\x1b\[[0-9;?]*[A-Za-z]", "", raw)
    return raw[-REPORT_CAP:]


def changed_tests(cell):
    """Which tests changed state, from the repository's own two runs.

    JUDGE.md asks for the names, not just the counts, because scope_discipline's
    unforgivable case is a test that stopped failing by being weakened.
    """
    def names(log):
        text = read(os.path.join(cell, log))
        return set(re.findall(r"^(FAILED|ERROR) ([^\s]+)", text, re.M))
    before, after = names("pytest-before.log"), names("pytest-after.log")
    fixed = sorted(name for _, name in before - after)
    broken = sorted(name for _, name in after - before)
    return fixed, broken


def main():
    cell = sys.argv[1]
    meta = json.load(open(os.path.join(cell, "meta.json")))
    fixed, broken = changed_tests(cell)
    patch = read(os.path.join(cell, "diff.patch"), PATCH_CAP).strip() or "no changes"
    report = report_of(cell, meta).strip() or "(the attempt said nothing)"

    out = [
        "<!-- blind judge input. JUDGE.md v1. The harness that produced this is",
        "     deliberately not named here; it is in meta.json beside this file. -->",
        "",
        "# ISSUE", "", read(os.path.join(cell, "prompt.txt")).strip(), "",
        "# PATCH", "", "```diff", patch, "```", "",
        "# TESTS", "",
        f"- before the attempt: {meta.get('passed_before')} passed, {meta.get('failed_before')} failed",
        f"- after the attempt:  {meta.get('passed_after')} passed, {meta.get('failed_after')} failed",
        f"- files changed in the working tree: {meta.get('changed_files')}",
        f"- tests that stopped failing: {', '.join(fixed) if fixed else 'none'}",
        f"- tests that started failing: {', '.join(broken) if broken else 'none'}",
        "",
        "# REPORT", "", report, "",
    ]
    with open(os.path.join(cell, "judge_input.md"), "w") as fh:
        fh.write("\n".join(out))
    print(f"judge_input.md: {os.path.join(cell, 'judge_input.md')}")


if __name__ == "__main__":
    main()
