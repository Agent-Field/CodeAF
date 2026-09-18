---
kind: added
title: a headless exec run leaves a pending judge record at its tail for the pool's sweep
pr: 1137
surface: [engine]
invalidates:
  - "A headless `codeaf exec` run was invisible to the Model Pool: `pool/pending.jsonl` carried no row for it, so no later start had anything to judge. The door now appends one row at its tail, under the `exec` door, and the restart-time sweep scores it like any other landing."
---

The pool's judge reaches a headless run the way it reaches a chat task — off
that run's own road, never on it. `exec` builds no session graph and so has no
live landing hook, so it writes the one thing that outlives the process: a
pending row carrying the run's model, answer, files and tokens, in the
`unverified` state a judge scores. Nothing waits on the judge; the write is one
`O_APPEND` of one line and the process exits at once. The row's id is the wall
clock in nanoseconds, because the sweep dedups on it and two runs must never
share one. A pool that forbids reading, or a run that never ran, writes nothing.
