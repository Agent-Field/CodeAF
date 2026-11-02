---
kind: fixed
title: codeaf factory Pause and Resume accept progress since the last read
pr: 2250
surface: [engine]
invalidates:
  - "Was true: progress could refuse Pause or Resume as stale, and Resume could leave no chat line. Is true now: progress alone never refuses these actions, Stop wins over a later Resume, and explicit Resume writes one matching chat line."
---

Pause and Resume accept the version the page already has. After Stop, Pause says
the job is not running and Resume says it is not paused; neither changes the job,
adds an activity receipt or starts spending. Pause also refuses a finished job.
Explicit Resume writes `resumed` once in the manager chat, including for a leased
job, without adding another line for a repeated press or automatic continuation.
