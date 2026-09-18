---
kind: added
title: the headless task door leaves a pending judge record at its tail
pr: 0000
surface: [engine]
invalidates:
  - "`codeaf run` — the headless door that runs one saved program with nobody watching and owns no session graph — left nothing behind for the Model Pool, so a program run outside a conversation was never scored: the chat door judged a task through a live landing hook, and this door had none. Its one ending (`reportSubharnessRun`) now appends the run's landing to `<profile>/pool/pending.jsonl` under the door `run`, where the restart-time sweep scores it on the next chat start."
  - "The row carries the run's model, its report, the files it wrote and its token count, with no high seat (this door has none) and a unique id drawn from the run's start in nanoseconds, so two runs never collide in the sweep's judged markers. A run that stopped incomplete leaves its row too, with whatever report it managed. A pool whose mode forbids reading writes nothing."
---

The last of the three headless doors reached for the pool. `codeaf run` runs one
saved program with nobody watching, so it has no task surface, no live landing
hook, and nothing to hand the judge when the work ends. It now builds the same
`session.TaskLanding` the chat door's hook is handed — at the one place its
endings are decided, `reportSubharnessRun`, so both the `--json` and the prose
paths leave exactly one row — and appends it to the pool's pending file for the
restart sweep (`poolrecord.go`). The process still exits at once: the row is one
`O_APPEND`, nothing waits on a judge, and a run whose pool cannot read writes
nothing at all.
