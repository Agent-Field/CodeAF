---
kind: fixed
title: a test binary is answered by a fresh machine, and never commits into the checkout it is running in
pr: 488
surface: [build, engine]
invalidates:
  - "`internal/lane` resolved its two files under the state root whenever nobody handed it a directory, so a test that named a fake ledger and nothing else was answered out of the router state of whoever started the suite: `TestAnEmptyLedgerIsAnEmptyChoice` was red for good on any box where a real run had written `~/.aforge/v3/lanes/<model>.json`. Under `go test` a path that would land in a root the process merely INHERITED now resolves to \"\" (internal/lane/undertest.go), which the store already reads as `ErrNoStore` and the sheet as no cache; a root a test CHOSE — `t.Setenv` of AFORGE_HOME, `store.at`, `sheet.cacheIn` — still works, and outside a test binary nothing changes at all."
  - "The gate on the call log from #352 compares against the LIVE state root; `internal/lane`'s captures the roots at package initialisation instead, because a lane test that points AFORGE_HOME at its own `t.TempDir()` and reads the store back is doing the correct thing and about fifteen of them do. The two gates refuse different things on purpose: what this one refuses is the root nobody chose."
  - "`TestNoTestWritesTheRealHome` was the whole of this directory's protection and it is syntactic — it asks whether a test function MENTIONS the registry — so a chooser that reaches the registry from inside walked past it. `TestEveryPathInThisPackageGoesThroughTheGate` now fails any file but undertest.go that names `home.Join`, `home.Dir` or `home.DefaultUnder`, which is what keeps the gate's coverage total for tests nobody has written."
  - "`exec.Cmd` reads an empty `Dir` as the calling process's own working directory, so a task that landed with no ground of its own ran `git commit` in the test binary's directory: one `go test ./internal/session/` run put \"task: Rewrite\", \"task: Measure\" and \"task: Paint\" onto the branch of the worktree it was launched from and they reached the remote. `gitWith` now refuses a command with nowhere to run, and internal/session's TestMain records its checkout's head and dirty set before the first test and reads them again after the last."
  - "An empty AFORGE_HOME for a whole suite run is not the fix and `scripts/one-suite.sh` does not export one: measured on clean dev, it turns `internal/lane` green and `internal/rtk` red (the managed `~/.aforge/bin/rtk` is gone) and `internal/resident` red as well. Packages disagree about what a state root should hold and each is right about its own subject, so isolation belongs at each package's own resolution seam. The wrapper does now pass `-count=1` when the caller named no count, so a whole-tree run cannot report a package it did not actually run."
---

The class is one sentence — a test reads or writes the state of whoever ran it —
and it has now cost a call ledger 356 rows of invented traffic (#286), a lane
test its permanent green (#475), and one session three commits on somebody
else's branch. Each fix is a gate at the single place a path is resolved, never
a rule the next test has to remember.
