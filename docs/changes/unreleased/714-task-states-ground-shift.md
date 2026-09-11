---
kind: changed
title: a task whose files moved under it asks you to resolve it, instead of saying nobody could check it
pr: 714
surface: [engine, chat]
invalidates:
  - "A landing whose ground moved — the work held its check, and your own branch changed the same files while it ran — read `nobody could check it` on the row, on the card and in the report, and offered `accept` / `not right`. It now reads `your branch changed the same files while it worked: a.go, b.go` and offers `resolve it` / `drop it`, which is the conflict's question and the conflict's answers."
  - "`TaskAskConflict` was reached only by a merge that would not fasten. It is now reached by two roads, and the fact that tells them apart is `TaskFacts.Shifted` / `TaskNotice.Shifted` — never the merge word, which stays `kept` on the shifted road because that branch WOULD have merged, and never the prose. The ask kinds are still the closed set of six."
  - "`Agent.groundShift` answered one sentence. It now answers the sentence and the moved files; `groundShiftReason` is still the sentence-only door."
  - "A shifted landing was handed to the model under `task.settle = auto`. It is not any more, for the reason a conflict is not — `handToModelOnAuto` reads the mark — and its note says `their own branch changed the same files while this worked, and that is not yours to accept` rather than borrowing the conflict's sentence, which claims a conflict that did not happen."
  - "`internal/manual/chat/how-tasks-run.md`'s section on another window changing the same file said the row reads `nobody could check it` and that `[a] accept` merges it. Both were wrong once this landed and both are corrected."
---

`Agent.ResolveConflict` needed no widening: its merge round never asked whether a
conflict marker had been written, so it was always the right verb for this road —
it brings your branch into the task's, checks the two changes together and lands
the work. What the shifted landing lacked was a card that offered it.
