#!/usr/bin/env python3
"""Turn a run's cell records into the scoreboard: a table for people, a CSV for
machines, and a comparison against the baseline when there is one.

TWO VERDICTS, NEVER ONE. Every row carries what the DOOR said about its own run
and, separately, what the pull request's TESTS said about the tree that door
left. A door that refuses work the tests call green is a defect of this product,
and folding the two into a single pass/fail is exactly how such a defect hides.
`pass` is their conjunction and is kept for the comparison against a baseline,
not for reading.

The same rows go to three places — the terminal, `rows.csv` beside the cells,
and the tracking issue with `--post` — so nobody can quote a number the file
does not carry. A regression is a judgement about ANCHORS only: a fresh pick
that fails on both doors is first a bad pick, and one that fails on one door is
a finding to read, not a red row to revert on.
"""
import argparse
import csv
import glob
import io
import json
import os
import statistics
import subprocess

# A NEW COLUMN IS APPENDED, NEVER INSERTED: a reader that takes rows.csv by
# position — an older baseline, somebody's spreadsheet — must still find every
# column where it has always been.
COLUMNS = ["run", "sha", "id", "door", "anchor", "pass", "wall_s", "cost_usd", "ttft_ms",
           "changed_files", "f2p_passed", "f2p_failed", "suite_passed", "suite_failed", "load", "reason",
           "door_verdict", "tests_verdict", "source", "tier", "gate_rounds", "task_done_s", "done_to_wall_s",
           "mark_fails", "carry_ons", "steward_last", "asked_s"]


def cells(run_dirs):
    """Every graded cell across the run directories given. A baseline may be
    assembled from several launches of the same binary — one door at a time,
    an anchor added later — and they are one scoreboard, not several."""
    found = []
    for run_dir in run_dirs:
        for path in sorted(glob.glob(os.path.join(run_dir, "cells", "*", "cell.json"))):
            with open(path) as handle:
                found.append(json.load(handle))
    return found


def row(meta, cell):
    f2p, suite = cell.get("f2p") or {}, cell.get("suite") or {}
    values = {
        "run": meta["run"], "sha": meta["sha"], "id": cell["id"], "door": cell["door"],
        "anchor": "yes" if cell.get("anchor") else "fresh", "pass": "pass" if cell["pass"] else "FAIL",
        "wall_s": cell.get("wall_s") or "", "cost_usd": "%.4f" % cell["cost_usd"] if cell.get("cost_usd") else "",
        "ttft_ms": cell.get("ttft_ms") or "", "changed_files": cell.get("changed_files", ""),
        "f2p_passed": f2p.get("passed", ""), "f2p_failed": f2p.get("failed", ""),
        "suite_passed": suite.get("passed", ""), "suite_failed": suite.get("failed", ""),
        "load": cell.get("load", ""), "reason": cell.get("reason", ""),
        # A cell recorded before these were written leaves them empty rather
        # than guessed: a verdict nobody measured is not a verdict.
        "door_verdict": cell.get("door_verdict", ""), "tests_verdict": cell.get("tests_verdict", ""),
        "source": cell.get("source", ""), "tier": cell.get("tier", ""),
        "gate_rounds": cell.get("gate_rounds", ""),
        "task_done_s": cell.get("task_done_s") if cell.get("task_done_s") is not None else "",
        "done_to_wall_s": cell.get("done_to_wall_s") if cell.get("done_to_wall_s") is not None else "",
        "mark_fails": cell.get("mark_fails") if cell.get("mark_fails") is not None else "",
        "carry_ons": cell.get("carry_ons") if cell.get("carry_ons") is not None else "",
        "steward_last": cell.get("steward_last") if cell.get("steward_last") is not None else "",
        "asked_s": cell.get("asked_s") if cell.get("asked_s") is not None else "",
    }
    # Missing measurements stay empty everywhere. In particular, older chat
    # rows may carry an explicit null gate count, which must not print `None`.
    return {name: "" if value is None else value for name, value in values.items()}


def provenance(r):
    """The row's provenance in one cell: which pool it came from, how large a
    change it asks for, and — only when the issue was not the picker's own find
    — where it was published, because an issue a model may have read before is
    a weaker witness than one nobody has seen."""
    parts = ["anchor" if r["anchor"] == "yes" else "fresh"]
    if r["tier"]:
        parts.append(r["tier"])
    if r["source"] == "swe-bench-verified":
        parts.append("verified")
    return "·".join(parts)


def table(rows):
    # `via` is which door was used; `door` is that door's own verdict on its
    # run and `tests` is the pull request's verdict on the tree it left. The two
    # are never merged, because a door that ends badly on work the tests call
    # green is a finding, and one column cannot say it.
    # The done-to-wall column exists because a chat that runs past its own
    # task's landing to the wall is a product defect (#468), and the fix must
    # be visible per anchor when it lands.
    head = ["issue", "via", "set", "door", "tests", "wall", "cost", "ttft", "files", "f2p", "suite", "gate", "done→wall", "marks", "carry", "load", "why"]
    out = ["| " + " | ".join(head) + " |", "|" + "---|" * len(head)]
    for r in rows:
        cost = "$" + r["cost_usd"] if r["cost_usd"] else ""
        wall = "%ds" % r["wall_s"] if r["wall_s"] != "" else ""
        ttft = "%sms" % r["ttft_ms"] if r["ttft_ms"] != "" else ""
        f2p = "%s/%s" % (r["f2p_passed"], r["f2p_failed"]) if r["f2p_passed"] != "" else ""
        suite = "%s/%s" % (r["suite_passed"], r["suite_failed"]) if r["suite_passed"] != "" else ""
        done_to_wall = "%ss" % r["done_to_wall_s"] if r["done_to_wall_s"] != "" else ""
        out.append("| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |" % (
            r["id"], r["door"], provenance(r), r["door_verdict"], r["tests_verdict"], wall, cost, ttft,
            r["changed_files"], f2p, suite, r["gate_rounds"], done_to_wall, r["mark_fails"], r["carry_ons"],
            r["load"], r["reason"]))
    return "\n".join(out)


def compare(rows, baseline):
    """Name every anchor that moved against the baseline's anchors: a lost pass,
    or cost or wall out of band. ANCHORS ONLY, whatever their source or tier: a
    fresh pick has no baseline to have moved from."""
    base = {(r["id"], r["door"]): r for r in baseline if r["anchor"] == "yes"}
    notes = []
    for r in rows:
        was = base.get((r["id"], r["door"]))
        if not was or r["anchor"] != "yes":
            continue
        if was["pass"] == "pass" and r["pass"] != "pass":
            notes.append("REGRESSION %s %s: passed at baseline, now %s" % (r["id"], r["door"], r["reason"]))
        if r["pass"] == "pass" and was["cost_usd"] and r["cost_usd"] and float(r["cost_usd"]) > 2 * float(was["cost_usd"]):
            notes.append("COST %s %s: $%s against $%s at baseline" % (r["id"], r["door"], r["cost_usd"], was["cost_usd"]))
        if r["pass"] == "pass" and was["wall_s"] and r["wall_s"] and int(r["wall_s"]) > 1.5 * int(was["wall_s"]):
            notes.append("WALL %s %s: %ss against %ss at baseline" % (r["id"], r["door"], r["wall_s"], was["wall_s"]))
    return notes


def totals(rows):
    """The anchors counted three ways: both verdicts good, the tests alone, the
    door alone. The last two are what separate a build that cannot fix the issue
    from a build that fixes it and then mishandles its own ending."""
    anchors = [r for r in rows if r["anchor"] == "yes"]
    both = sum(1 for r in anchors if r["pass"] == "pass")
    green = sum(1 for r in anchors if r["tests_verdict"] == "green")
    ok = sum(1 for r in anchors if r["door_verdict"] == "ok")
    spend = sum(float(r["cost_usd"]) for r in rows if r["cost_usd"])
    walls = [int(r["wall_s"]) for r in rows if r["wall_s"] != ""]
    longest = max(walls) if walls else 0
    return ("anchors: %d/%d both-good · tests green %d/%d · door ok %d/%d"
            " · %d cells · spend $%.3f · longest cell %ds") % (
        both, len(anchors), green, len(anchors), ok, len(anchors), len(rows), spend, longest)


def read_csv(path):
    with open(path, newline="") as handle:
        return list(csv.DictReader(handle))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("run_dir", nargs="+", help="run directories; the first one's run.json names the run and holds the outputs")
    parser.add_argument("--baseline", help="rows.csv of the run to compare against")
    parser.add_argument("--post", help="tracking issue number to append the scoreboard to")
    parser.add_argument("--repo", default="Agent-Field/aforge-v2")
    parser.add_argument("--note", action="append", default=[], help="a caveat printed under the table, repeatable")
    args = parser.parse_args()

    first = args.run_dir[0]
    with open(os.path.join(first, "run.json")) as handle:
        meta = json.load(handle)
    rows = [row(meta, cell) for cell in cells(args.run_dir)]
    with open(os.path.join(first, "rows.csv"), "w", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=COLUMNS)
        writer.writeheader()
        writer.writerows(rows)

    notes = compare(rows, read_csv(args.baseline)) if args.baseline else []
    body = io.StringIO()
    body.write("### canary · `%s` · %s · %s\n\n" % (meta["sha"], meta["run"], meta["model"]))
    body.write("binary `%s` · wall %ss · cap $%s · doors %s\n\n" % (meta["bin"], meta["wall"], meta["cap"], meta["doors"]))
    body.write(table(rows) + "\n\n**" + totals(rows) + "**\n")
    for note in args.note:
        body.write("\n- " + note + "\n")
    if args.baseline:
        body.write("\nagainst baseline `%s`: %s\n" % (args.baseline, "; ".join(notes) if notes else "no anchor moved"))
    csv_text = io.StringIO()
    writer = csv.DictWriter(csv_text, fieldnames=COLUMNS)
    writer.writeheader()
    writer.writerows(rows)
    body.write("\n<details><summary>rows.csv</summary>\n\n```csv\n" + csv_text.getvalue() + "```\n</details>\n")
    text = body.getvalue()
    print(text)
    with open(os.path.join(first, "scoreboard.md"), "w") as handle:
        handle.write(text)
    if args.post:
        subprocess.run(["gh", "issue", "comment", args.post, "--repo", args.repo, "--body-file",
                        os.path.join(first, "scoreboard.md")], check=True)


if __name__ == "__main__":
    main()
