---
kind: added
title: pool status says what is waiting for a judge and what the last sweep did
pr: 1142
surface: [engine]
invalidates:
  - "`codeaf pool status` counted only the outbox's rows and named the last live judge. It now also counts the unjudged rows in the pending file the headless doors leave — `pending judge: 1 · oldest do run 3h` — prints the restart sweep's own record — `last sweep: 2m ago · judged 3 · 2 still pending · 41s of 10m` — and carries both as `pending_judge` and `last_sweep` in --json beside the outbox and judge records. An install with nothing waiting and no sweep yet says `pending judge: none` and `last sweep: none yet`."
  - "Pending rows carry the moment they were written (`at` in `pool/pending.jsonl`), which their age in status is read against; rows written before the stamp existed read as zero and say an unknown age."
---

The restart sweep judged the headless doors' waiting rows at chat start but left
no trace of itself, and nothing counted the rows it existed for, so a person
could not tell whether their `exec`/`run`/`do` runs were being scored at all.
`poolJudgeSweep` now leaves one record of itself at its end (`pool/sweep-last.json`:
`{at, judged, left, budget_used, cut}`, where `cut` means the deadline ended the
sweep with unjudged rows left behind) beside the per-landing judge record, and
`pool status` reads it into one line in the last-judge line's shape, with the
pending file's unjudged rows and the oldest waiting run's door and age above it.
The count is of the pending file alone — the outbox's `pending` count keeps its
meaning and sits beside it.
