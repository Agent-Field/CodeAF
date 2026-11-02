---
kind: fixed
title: A factory plan step is told the file-list shape Auto checks, and a plan without one says so plainly
pr: 2096
surface: [engine, docs]
invalidates:
  - "A plan step was never told the one shape of file list the factory reads (a `Files:` line, then a `- path` line per file), so a plan that named its files in its own words (a numbered \"CHANGE, PER FILE\" list) read as naming none, and under any protected path Auto paused a job that touched nothing protected. Is true now: every step called plan, the shipped one and a pipeline file's own, is told that shape in its brief, carried on or fresh. The parser is unchanged and still reads nothing else."
  - "Was true: a plan with no file list under a protected path held as `protected · the plan names no files`, said as \"A protected path: the plan names no files\". Is true now: the hold names the protected paths in force, `protected · auth/, infra/ · the plan names no files`, and Auto's reason (`autonomy_why`) says \"The plan didn't list its files, so the factory couldn't check them against auth/, infra/. Approve, or send it back to list them.\" A hold saved before this reads \"… against its protected paths\". A matched file still holds and reads as before."
---

Nothing sends the plan back on its own: approving or sending it back stays the
person's. A path two rules protect is named once.
