---
kind: fixed
title: three identical timeouts of one command end a leaf's round, with a reason that names the command
pr: 629
surface: [engine, docs]
invalidates:
  - "Nothing stopped a leaf from re-running a command that had already timed out. A hanging test could be run three times, and the leaf went on working until the run's wall ended it — three identical timeouts of one command cost 180 s of a 900 s wall, and the run reported a wall rather than the delivery whose tree was already correct and green. The same command, with the same timeout, timing out three times in one round now ends that round; `exec.StopToolTimeouts` (\"tool-timeouts\") is the reason, and the leaf is not asked for another turn."
  - "The no-progress guard was the only thing watching a leaf repeat itself, and it cannot see this: `progressGuard.observe` excludes error results on purpose, because a model retrying a failing call is troubleshooting rather than spinning, and a timeout is an error result. `internal/exec/tooltimeout.go` is a second, narrower counter beside it, keyed on the tool name plus the call's exact argument text and kept for the whole round rather than as a consecutive streak — the timeouts that cost the replication its wall were transcript turns 29, 30 and 38."
  - "`exec.Result` carried the timeout only inside the sentence `command timed out after Ns`, so any reader had to match person-facing text to learn the fact. It carries an unexported `timedOut` flag now, set where the command runner builds that result, and the round limit reads the flag."
  - "`store.LeafExhausted.Bound` could only be `deadline`, `turn-cap`, `budget` or `overrun`, and only `cmd/aforge`'s `journalLeafExhaustion` ever wrote the row — gated on `leafRanOutOfRoom`. `internal/exec` now writes it directly for this one ending, the way `selfclose.go` already writes its own row, so the headless `⏳` line and `revision.remainderSubject`'s `HOW IT STOPPED` both say why with no change at either call site."
  - "This ending is deliberately NOT out of room: `exec.StopReason.OutOfRoom` and `Outcome.Overran` are both false for it, so `resident.ExecResult.RanOut` stays false and the leaf keeps its account. A leaf stopped this way is judged on the work standing on its tree, not treated as evidence for a next attempt."
  - "No command's own timeout moved. `sh`'s `t` still defaults to 60 seconds, and nothing gives up sooner than it did; the rule only refuses to start one command again after it has already reached its timeout three times in the round."
---

The threshold is one constant, `toolTimeoutRepeatCap`, interpolated into the
sentence a person reads — `the same command timed out 3 times, so it was stopped
rather than run again: <command>` — and quoted in `internal/manual/chat/adaptive-runs.md`
beside the repeat guard, the other rule that ends repetition.

`PERF.md` records the three-timeout cap beside the other leaf bounds.
