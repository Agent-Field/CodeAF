---
kind: fixed
title: a reopened conversation keeps its tasks, and a task file that cannot be read is never overwritten
pr: 1213
surface: [chat, engine]
invalidates:
  - "A checkpoint that failed its schema check was dropped whole and the session's next save landed on the same path, so the unreadable file was replaced by an empty one. It is now moved beside itself as `tasks.json.refused-<seconds>` first, and the id counter is raised past every task that left a transcript or a working copy on disk."
  - "Every node in `tasks.json` had to carry an acceptance or the whole file was refused. A task that came out of a run's plan is admitted with the plan store's id and no acceptance of its own, so with the belt on the first such task made the file unreadable on the next open. A plan-born node now reads back without one."
---

The owner closed and reopened a conversation that had run twenty tasks and the
rail came back empty. The engine's log said `ignoring corrupt task checkpoint …
node 5 has no acceptance`; the file had been 42 KB, the next save made it 1.8
KB with `"nodes": null`, and the counter restarted, so new rows answered to
numbers the transcript already used for other tasks. The transcripts, the
working copies and the plan store were all still on disk; only the sheet that
indexes them was gone.
