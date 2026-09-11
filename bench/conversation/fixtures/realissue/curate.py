#!/usr/bin/env python3
"""Derive a task prompt from an issue body by keeping only the sections a person
reporting the bug would have written, and record exactly what was dropped.

Issues in this repository are engineering reports: `## Where` carries the
diagnosis, `## The fix` carried a literal Go patch in #528, and `## Acceptance`
names the very tests the fix adds. A verbatim body is therefore not a task. This
splits on ATX headings, keeps the symptom, the reproduction and the stated law,
drops the rest, and hands back the derivation record the manifest stores.

    curate.py <issue-body-file>        # prints the derived prompt and its record

It is mechanical and it is not a sanitiser: manifest.py's lint still runs over
the result, and the lint is lint, not proof.
"""
import hashlib
import json
import re
import sys

# Kept: what happened, how to see it again, and what the behaviour should be.
KEEP = re.compile(r"^(what happened|observable failure|problem\b.*|replication|"
                  r"reproduction|the law|law|impact|symptom.*)$", re.I)
HEADING = re.compile(r"^(#{2,3})\s+(.*?)\s*$", re.M)
BRIEF = ("You are handed a report from the person who hit this. Reproduce it, find the "
         "cause, and fix the source. Do not change any test to agree with the code.\n")


def curate(body, title, drop_paragraphs=()):
    """-> (prompt text, derivation record).

    Sections are kept or dropped whole by heading. `drop_paragraphs` then removes
    individual blank-line-separated paragraphs by regular expression, because a
    report writes its diagnosis wherever it likes: #528 named the missing switch
    arm inside `## What happened`, which no heading rule can reach. Each pattern
    and how many paragraphs it took are recorded, and a pattern that matches
    nothing is an error rather than a silent no-op.
    """
    marks = [(m.start(), m.group(2)) for m in HEADING.finditer(body)]
    bounds = [(head, body[start:(marks[i + 1][0] if i + 1 < len(marks) else len(body))])
              for i, (start, head) in enumerate(marks)]
    preamble = body[:marks[0][0]] if marks else body
    kept, dropped, chunks = [], [], ([preamble.strip()] if preamble.strip() else [])
    for head, chunk in bounds:
        if KEEP.match(head):
            kept.append(head)
            chunks.append(chunk.strip())
        else:
            dropped.append(head)
    body_text = "\n\n".join(chunks)
    taken = {}
    for pattern in drop_paragraphs:
        paragraphs = body_text.split("\n\n")
        keep = [p for p in paragraphs if not re.search(pattern, p, re.I | re.S)]
        taken[pattern] = len(paragraphs) - len(keep)
        if not taken[pattern]:
            raise ValueError("drop_paragraphs pattern matched nothing: " + pattern)
        body_text = "\n\n".join(keep)
    text = BRIEF + "\n# " + title + "\n\n" + body_text + "\n"
    record = {"kept_sections": kept, "dropped_sections": dropped,
              "dropped_paragraphs": taken,
              "rule": "ATX sections kept by heading allowlist (symptom, reproduction, "
                      "stated law); diagnosis, fix and acceptance sections dropped whole",
              "source_sha256": hashlib.sha256(body.encode()).hexdigest()}
    return text, record


if __name__ == "__main__":
    body = open(sys.argv[1]).read()
    text, record = curate(body, "<title>")
    print(text)
    print(json.dumps(record, indent=2))
