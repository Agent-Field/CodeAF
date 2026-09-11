#!/usr/bin/env python3
"""summary.py — one page about one run, written where the evidence is.

It is deliberately not a comparison. `compare.py` is the file that puts two runs
beside each other and rules on parity; this one says what a single run measured,
so that a run whose other half never happened is still readable on its own.

Usage: summary.py <run-dir>   → Markdown on stdout
"""

import json
import os
import statistics
import sys


def load(path, default=None):
    try:
        with open(path, "r", errors="replace") as handle:
            return json.load(handle)
    except (OSError, ValueError):
        return default


def jsonl(path):
    out = []
    try:
        handle = open(path, "r", errors="replace")
    except OSError:
        return out
    with handle:
        for line in handle:
            line = line.strip()
            if not line:
                continue
            try:
                out.append(json.loads(line))
            except ValueError:
                continue
    return out


def figure(value, spelling="{:,}"):
    """UNKNOWN RENDERS AS NOTHING. The emptiness law is a repository-wide design
    law and it applies to a report as much as to a screen: a dash says the run
    did not measure this, where a zero would say it measured nothing."""
    if value is None:
        return "—"
    return spelling.format(value)


def middle(values):
    values = [value for value in values if isinstance(value, (int, float))]
    return round(statistics.median(values), 1) if values else None


def main():
    root = sys.argv[1]
    meta = load(os.path.join(root, "meta.json"), {}) or {}
    out = []
    add = out.append

    add("# %s — prompt-diet bench" % meta.get("label", "run"))
    add("")
    add("| | |")
    add("| --- | --- |")
    for key in ("branch", "revision", "model", "rig_sha", "layers", "started", "finished"):
        if meta.get(key):
            add("| %s | `%s` |" % (key.replace("_", " "), meta[key]))
    add("")

    add("## The static prefix (layer A)")
    add("")
    add("| piece | bytes |")
    add("| --- | ---: |")
    add("| page | %s |" % figure(meta.get("prefix_prompt")))
    add("| tool block | %s |" % figure(meta.get("prefix_tools")))
    add("| **total** | **%s** |" % figure(meta.get("prefix_bytes")))
    add("")

    outcomes = load(os.path.join(root, "suites", "outcomes.json"))
    if outcomes:
        add("## Outcomes (layer B)")
        add("")
        add("| suite | pass | fail | skip | incomplete |")
        add("| --- | ---: | ---: | ---: | ---: |")
        for suite in sorted(outcomes):
            tally = outcomes[suite]["tally"]
            add("| `%s` | %d | %d | %d | %d |" % (
                suite, tally["pass"], tally["fail"], tally["skip"], tally["incomplete"]))
        add("")
        failed = [(suite, test)
                  for suite in sorted(outcomes)
                  for test, entry in sorted(outcomes[suite]["tests"].items())
                  if entry["outcome"] in ("fail", "incomplete")]
        if failed:
            add("Not green:")
            add("")
            for suite, test in failed:
                add("- `%s` — %s" % (test, outcomes[suite]["tests"][test]["outcome"]))
            add("")

    cells = jsonl(os.path.join(root, "cells", "conversation", "results.jsonl"))
    if cells:
        add("## Conversation cells (layer C)")
        add("")
        add("| scenario | door | verdict | wall s | cost $ | in | out | turns |")
        add("| --- | --- | --- | ---: | ---: | ---: | ---: | ---: |")
        for row in cells:
            add("| %s | %s | %s | %s | %s | %s | %s | %s |" % (
                row.get("scenario"), row.get("door"), row.get("verdict"),
                figure(row.get("wall_s"), "{}"), figure(row.get("cost_usd"), "{:.4f}"),
                figure(row.get("tokens_in")), figure(row.get("tokens_out")),
                figure(row.get("turns"), "{}")))
        add("")

    e2e_csv = os.path.join(root, "cells", "e2e.csv")
    if os.path.exists(e2e_csv):
        add("## Task cells (layer C)")
        add("")
        add("```")
        with open(e2e_csv, "r", errors="replace") as handle:
            add(handle.read().rstrip())
        add("```")
        add("")

    wire = jsonl(os.path.join(root, "wire.jsonl"))
    if wire:
        guard = [row for row in wire if row["source"] == "guard"]
        calls = [row for row in wire if row["source"] == "calllog"]
        add("## The wire (layer D)")
        add("")
        add("| | requests | median prompt tok | median cached tok | median completion tok | total $ |")
        add("| --- | ---: | ---: | ---: | ---: | ---: |")
        for name, group in (("guard", guard), ("call log", calls)):
            if not group:
                continue
            spend = sum(row["cost_usd"] for row in group
                        if isinstance(row.get("cost_usd"), (int, float)))
            add("| %s | %d | %s | %s | %s | %s |" % (
                name, len(group),
                figure(middle([row.get("prompt_tokens") for row in group])),
                figure(middle([row.get("cached_tokens") for row in group])),
                figure(middle([row.get("completion_tokens") for row in group])),
                figure(spend, "{:.4f}")))
        add("")
        blocks = [row.get("tool_block_bytes") for row in calls]
        if any(isinstance(value, int) for value in blocks):
            add("Tool block on the wire, median: %s bytes." % figure(middle(blocks)))
            add("")

    sys.stdout.write("\n".join(out) + "\n")


if __name__ == "__main__":
    main()
