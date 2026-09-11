---
kind: fixed
title: a task's report keeps the fenced tail it promised, and says when it was cut
pr: 639
surface: [engine, chat]
invalidates:
  - "A task's report was its final message cut to the first three non-empty lines by a plain line count, so a reply that spent two lines on a sentence and opened a code fence on the third reported the sentence, the opening fence, and nothing else. A fence opened inside those three lines is now carried through its closing fence, up to eight further lines."
  - "A report that dropped lines was indistinguishable from a complete one — `firstLines` marked nothing. A report that leaves any non-empty line behind now ends on `…` on a line of its own, and so does one whose block the report had to close itself; a report that carries the whole message unaltered still carries no mark."
  - "A report could end on a bare `` ``` ``. It never can now: a block that will not close within its room is closed by the report, and an opening fence with nothing carried under it is dropped instead of left hanging."
  - "`internal/manual/chat/how-tasks-run.md` said a report was the final message cut to three non-empty lines of three hundred characters and stopped there. It now states the fenced-block exception and its eight lines, the cut mark, and where the whole final message still is."
  - "`taskReport` no longer calls `firstLines`; it calls `composeTaskReport`, and `taskReportFenceLines` and `taskReportCut` are new beside `taskReportLines` and `taskReportLineLimit`. `firstLines` is unchanged and its other callers — a judge's shape, an adaptive run's digest, the sentence a missing check reports — behave exactly as before."
---

Issue #577. The report and the node's journal disagreed by construction rather
than by data loss: the whole reply was in hand at the call site that composed
the report and at the line after it, which re-read the same text to find the
files the node declared. What the person read off the card was three lines of a
promise with the promised lines cut away and nothing saying so.

`orchestrateDigest` cuts an adaptive run's node digest with `firstLines` and has
the same shape of bug against a different budget. It is deliberately untouched
here and wants an issue of its own.

Review follow-up: `PERF.md` now records the report's ordinary byte and line
bounds and its new eight-line fenced exception, so the increased report room
is reviewable beside the other performance limits.
