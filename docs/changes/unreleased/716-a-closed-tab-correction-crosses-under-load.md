---
kind: fixed
title: The tui3 harness only gives up on a command that may never answer
pr: 716
surface: [chat]
invalidates:
  - "internal/tui3's test harness gave EVERY command 150ms (`cmdBudget`) and returned nothing for one that answered later, without saying so. That was believed to cost nothing because the waiters answer in microseconds — and it did, until the box was busy: a record write over 150ms left `typeSteer` handing back no crossing at all, and the next assertion printed `no send crossed`, a sentence accusing the surface of losing a person's correction it had in fact delivered. NOW `budgetFor` prices by what a command can do: bubbletea's tick keeps `tickBudget`, the waiters `blockingCommands` names keep `cmdBudget`, and everything else — the disk writes, the folder reads, the small computations, all of which finish — gets `workBudget`. A healthy run never pays it: over a whole-package run of 12045 commands, all 3364 drops were the tick or a named waiter, and the slowest ordinary answer took 14ms."
  - "A command the harness gave up on disappeared silently, so a test that needed its message failed somewhere else, about something else. NOW crossing `workBudget` is a hang: `droppedWork` names the stuck command's runtime symbol and the real wait, from all three places a command can be given up on."
  - "`blockingCommands` was described as a table that prices nothing and only says which commands may run beside each other. It now has two readers and one meaning — these are the commands that may never answer — and `waiterSymbol` is the single reading of it that `budgetFor` and `overlappable` both ask."
  - "A test in `internal/tui3` that failed only when the whole package's binary ran was read as a load-shaped flake to rerun. `TestAClosedCorrectionSurvivesRestartWithoutRestoringPlainDrafts/close_kept` was that shape at roughly 1 run in 40, and it was a real defect in the harness, found by measuring where the harness spends its wall clock rather than by rerunning it."
---

The failure this fixes never touched the shipped surface. A correction typed into
a task, with the tab then closed on `keep running`, crosses, survives the close
and comes back as a correction — driven on the real binary to check, and the
worker's own answer cites the words. What was broken was the harness that reads
the surface, and the sentence it let a test print about a person's lost words.

Everything here is a `_test.go` file. No page, no prompt, no shipped code.
