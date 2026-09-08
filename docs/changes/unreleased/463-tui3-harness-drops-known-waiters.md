---
kind: fixed
title: the tui3 harness stops waiting on ticks that cannot reach it and names every command that parks
pr: 463
surface: [chat]
invalidates:
  - "internal/tui3's test harness gave every command one 150ms budget. bubbletea's tick
    now gets 120ms instead — above the longest tick the surface has that fires (100ms)
    and below the 150ms debounce — so the polls it could never deliver are no longer
    waited on. Every other command, the waiting ones included, keeps the unchanged 150ms,
    and there is deliberately no knob to shorten a waiter's budget."
  - "It was believed that the tui3 test fakes never feed the surface's wait channels, so a
    recognised waiter could be answered with nil at once. That is false and was measured:
    waitEvent answers 28 of 118 calls, bubbletea's tick fires on 451 of 593, and dropping
    the recognised names outright fails more than thirty tests. The fakes hand back a
    buffered channel and the events in it are what the streaming tests assert on."
  - "internal/tui3 was believed to hang under load, and TestTheStripCannotOutliveTheRowItWasOpenedOn
    to be the test at fault, so the package was thought to need a longer CI ceiling.
    Neither was true: the strip test ran in 3.62s every time and the whole binary crossed
    CI's eight-minute ceiling, so whichever test was in flight at the cliff got named. The
    package now finishes in 452s with all 2724 tests passing — 28s under that ceiling on a
    loaded box, which is not yet a margin."
  - "A waiting command added to internal/tui3 used to be known to nobody.
    TestTheHarnessKnowsEveryCommandThatCannotAnswer now reads the surface's own source and
    fails, naming the function and its file, when a function shaped like a waiting command
    — name beginning wait, pump or watch, single tea.Cmd result — is missing from the
    blockingCommands table in tui3_test.go."
---

Three quarters of the package's wall clock was the harness sleeping: 2415 commands dropped
at 150ms each, 362s of 478s. Only the tick part of that is recoverable without betting on
the scheduler, and this change takes that part. The rest, and the roughly 950 goroutines
still parked at the end of a run (`TestMain` reports the count), wait on #467: fakes that
own and close their own channels, so a waiter ends rather than parks. `blockingCommands` is
the enumeration that change works from.
