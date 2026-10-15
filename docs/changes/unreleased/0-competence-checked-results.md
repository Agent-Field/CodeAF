---
kind: added
title: codeaf keeps what each model has proven by declared checks that ran, and serves it as competence.map / competence.evidence and codeaf competence --json
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "No record tied a model, a place and a real check outcome together: the project index's verdictBasis was declared but never filled, a checker's verdict is a model's judgement, and the run road's per-check exits lived only in plandb.db, which deleting the task removes. A settled run's declared-check exits (plandb VerdictBasis kind run) are now written, with the one model that answered its work, to <home>/v3/checked-results.jsonl. Work finished before this has no evidence on it and none is inferred."
  - "A run-road worker's plandb spend row named the model the seat was BUILT on, even when its calls were answered by a fallback. internal/run now writes one spend row per model that answered (session.Agent.AnsweredUsage, tallied at the one bank door); the rows sum to the same figures, so every price reader is unchanged."
  - "`codeaf competence` had no JSON and only the resident's territory/profile view (unchecked done counted as success, stale after 90 days). `--json [--model id] [--scope id]` now prints the checked-results map the app draws (stale after 30 days, a failed check counts against); the text output is unchanged."
  - "Deleting a task or a conversation removed every trace of how its checks came out. Checked results now outlive it and are reported with source_missing."
  - "The task kind (`taskKindOf`/`taskKindsMeet`) lived only in internal/session's taskgrade.go. It is now internal/competence's WorkOf/WorkMeets, read by the task ledger unchanged; the competence map groups by the exact kind, not the overlap."
---

Not recorded, on purpose: a run another approved task joined (no fact says which
check proved which goal), a run answered by more than one model or by an unnamed
one, and undo — `quick.undo` takes back quick tasks, which run no declared checks.
The app wire is in docs/design/app-v3/PROTOCOL.md under V76; an engine without it
answers `unknown_method`. Both reads are bounded (newest 32 MiB, 10 s, ≤300 scopes)
and call no model.
