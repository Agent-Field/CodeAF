---
kind: fixed
title: Held work, kept branches and spend limits say what is true
pr: 1670
surface: [chat, engine, remote, docs]
invalidates:
  - "A chat turn or a chat task past today's spending limit was refused with a bare line, or a task said it started and then sat held. Both now ask first with a card to raise the limit for today or stop, and the top bar shows a raised limit at once."
  - "The read hand-off helper could write, commit and merge. It is now read-only, and a failed hand-off no longer leaves a stopped card for a helper that never started."
  - "Every check in a run drew from one shared ceiling, so later checks stopped having spent almost nothing, and a run with unfinished checks could end as done. Each check now has its own ceiling, and unfinished checks fail the run and are named."
  - "A kept task or run branch was invisible to /land, and the landing line gave no reason. /land now lists it, reaches the engine over the local host, and says why the branch was kept."
  - "A scheduled firing that only reported a sentence was logged as landed. It now comes to said."
---
