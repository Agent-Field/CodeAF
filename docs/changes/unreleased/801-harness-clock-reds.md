---
kind: fixed
title: The four tests #798's harness clock left red are green, and a drop wakeup with nothing new is the quiet window
pr: 801
surface: [chat]
invalidates:
  - "A `internal/tui3` test whose claim is a beat — `TestEnterConnectsOpenRouterInTheBrowserAndHandsTheKeyToThisProcess`, `TestAJobsLogReaderTakesOneLastReadingAfterTheJobEnds` — got `<nil>` from the harness clock for any tick longer than `tickBudget`. It waits the beat out with `waitOut(cmd)`, which costs no wall time and puts the budget back for the next command."
  - "`dropSettled` asked the clock on every wakeup whether the quiet window had passed, so under a clock that delivers the wakeup without moving (the harness) a drop never settled and `TestAPathInTheBoxIsNeverChipped` / `TestAnAbsolutePathDoesNotHoldTheCommandListOpen` timed out. It asks the clock only when a character arrived after the wakeup was armed; a wakeup that finds nothing new is the quiet window by construction."
---

Red on `dev` from `f96c7c96f` (#798) through `436aa8426`, verified by name on the
Spark; green at `b9142e747`. Nothing a person sees changes.
