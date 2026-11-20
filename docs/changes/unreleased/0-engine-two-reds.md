---
kind: internal
title: the go-ahead factory test runs on its own clock, and the Spending tab takes its shortfall rows once per visit
pr: 0
surface: [engine, chat]
invalidates:
  - >-
    cmd/codeaf TestADeskGoAheadStartsTheCoveredJobAfterItsParentMerges was
    believed broken by a canvas/connected change. It was a date bomb that went
    red on dev and canvas/connected alike at 2026-11-20 00:00 UTC. Its fixed
    allowance end was compared against the wall clock, because the test never
    gave the factory server its clock. The server now reads the test's clock
    (serve.Config.Now), and the product is unchanged.
  - >-
    internal/tui3 TestTheSpendingTabIsTheSevenRowsAndNothingElse was believed
    to be a timing flake. The Spending tab asked the process-wide unwritten and
    unbilled counters on every build, and an earlier test in the same binary
    can leave the unbilled count above zero. The tab grew an `unbilled` row
    depending on test order. The sheet now takes both readings once, when the
    panel opens, as it already took `today`. The test helper pins them to a
    machine with no shortfall, and the test bumps the counter itself to prove
    it.
---
