---
kind: added
title: codeaf keeps what each model has proven by declared checks that ran, and serves it as competence.map / competence.evidence and codeaf competence --json
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "No record tied a model, a place and a real check outcome together: the project index's verdictBasis was declared but never filled, a checker's verdict is a model's judgement, and the run road's per-check exits lived only in plandb.db, which deleting the task removes. A settled run's declared-check exits (plandb VerdictBasis kind run) are now written, with the one model that answered its work, to <home>/v3/checked-results.jsonl. Work finished before this has no evidence on it and none is inferred."
  - "A run-road worker's plandb spend row named the model the seat was BUILT on, even when its calls were answered by a fallback, and nothing said so. spend has a new `answered` column (0 for every old row and every AddSpend): internal/run writes one AddAnsweredSpend row per model that gave an answer (session.Agent.Answers, counted where each response arrives, even one that cost nothing or carried no usage; late receipts add money, never an answer) carrying that model's banked share (session.Agent.AnsweredUsage). Money with no named answer, and calls or tokens the shares do not exactly cover, stay unmarked rows (the remainder on the built-on model). The rows sum to the same figures, so every price reader is unchanged."
  - "The bare bash tool answered a command killed by a signal (exit -1 from Go) as a SUCCESS, so a run's trajectory recorded a killed check as exit 0. It now answers the shell's 128+signal (137 for SIGKILL) as a failure, a process that exited 0 but held its pipes past WaitDelay as 0, and a wait with no exit status at all as `Command ended without an exit status`, a failure. Background jobs report the same codes."
  - "`codeaf competence` had no JSON and only the resident's territory/profile view (unchecked done counted as success, stale after 90 days). `--json [--model id] [--scope id]` now prints the checked-results map the app draws (stale after 30 days, a failed check counts against); the text output is unchanged."
  - "Deleting a task or a conversation removed every trace of how its checks came out. Checked results now outlive it and are reported with source_missing."
  - "The task kind (`taskKindOf`/`taskKindsMeet`) lived only in internal/session's taskgrade.go. It is now internal/competence's WorkOf/WorkMeets, read by the task ledger unchanged; the competence map groups by the exact kind, not the overlap."
---

Not recorded, on purpose: a run another approved task joined (no fact says which
check proved which goal), a run answered by more than one model (a free answer
counts) or by an unnamed one, a run with any declared check the shell could not
start (126/127 or negative — never folded as a pass from the rest), and undo — `quick.undo` takes back quick tasks, which run no declared checks.
The app wire is in docs/design/app-v3/PROTOCOL.md under V76; an engine without it
answers `unknown_method`. Both reads are bounded (newest 32 MiB, 10 s, ≤300 scopes)
and call no model.
