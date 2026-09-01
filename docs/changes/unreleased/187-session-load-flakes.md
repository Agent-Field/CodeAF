---
kind: fixed
title: two task nodes can no longer be handed one journal, which is what made five session tests flake
pr: 187
surface: [engine]
invalidates:
  - "`internal/session TestOnlyADesignsOwnThreadCarriesTheReviseVerb`, `TestInterruptedTurnDoesNotWakeOnTheNoteItDrained` and `TestAChangeWithdrawsTheCardRewritesThePageAndAsksAgain` were known-red, described as \"flake under full-suite load, pass alone — rerun them in isolation before believing a failure\". All three are out of `.github/known-red.txt` and out of CLAUDE.md, and the advice is reversed: an `internal/session` test that fails only while other suites run beside it is a bug report now, not a known shape."
  - "A node's journal was named `<stamp>_<id>.jsonl` with the stamp good only to the SECOND, and `taskJournalPath`'s own comment made uniqueness the CALLER's job to add through its suffix. The stamp carries microseconds now and is minted through `journalMoment`, which never answers the same microsecond twice in one process — so no caller has to add anything, and `task_audit.go`'s nonce is a nonce for its own sake rather than a collision guard."
  - "Two agents could be handed one journal path. A session with no file of its own is named by the constant `unfiled` and node ids restart at one in every graph, so two nodes minted inside one second collided: the second agent either resumed the first one's transcript or, while the first still held the file's flock, was refused with a `SessionLockedError` and the work it was starting never ran at all."
  - "`TestTheDesignStartedEventNamesTheResolvedModel` returned as soon as it had read the first event, leaving a whole design agent running. It reads the design's card and declines it, because a test that walks away from a job leaves it writing into a temporary directory the cleanup is about to remove."
---

Five `internal/session` tests failed only when other suites were running beside
them, and five tests that fail only under load are one cause, not five
coincidences. The cause was the journal path. Every test in the package runs
under one HOME, every test agent has no session file, so the session segment is
the constant `unfiled` for all of them and node ids start again at one in every
graph — which left a timestamp good to the second as the only thing telling two
nodes' transcripts apart.

Load did not create the collision; it widened the window. A design runs on a
goroutine that outlives the turn that asked for it, and on a busy machine it
holds its journal across more of its neighbours.

The three remaining fixes are in tests and are the same lesson in smaller
print: a hand's finishing order came from `time.Sleep(15 * time.Millisecond)`
rather than from a signal, and two parked-parent tests landed their parts at
once and raced the delivery against the loop reading it. Sleeps expire on a wall
clock while the goroutines behind them wait for a processor, so on a loaded
machine every one of them was measuring the machine.
