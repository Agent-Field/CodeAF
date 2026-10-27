---
kind: fixed
title: Save task progress and desktop settings while Windows readers have them open
pr: 1999
surface: [engine]
invalidates:
  - "A brief Windows reader could prevent a task checkpoint or desktop state update from being saved. Replacements now wait a bounded time for that reader to close."
---

Task progress, including a finished task's cost and tokens, is saved while the
app briefly reads its files on Windows. Session and project records, desktop
settings and receipts use the same bounded wait. A lasting write failure keeps
its existing error handling; files are never deleted to force a replacement.
