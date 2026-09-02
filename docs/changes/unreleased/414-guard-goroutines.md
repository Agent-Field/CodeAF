---
kind: fixed
title: The goroutine sweep reads the defers, and every bare spawn in the guarded tree runs under guard.Go
pr: 414
surface: [chat, engine, build]
invalidates:
  - "Eight goroutines in the guarded tree (the quorum verifiers, the host follow pump, the once-per-launch home sweep, lanes.Persist, the bare executor's wait and cancel watchers, the write-lock transaction attempt and its abandoned rollback) ran without a recover and could take the terminal down. All run under `guard.Go` now, as `chat/quorum-verify`, `chatv3/host-follow`, `chatv3/sweep-home`, `lanes/persist`, `exec/bare wait`, `exec/bare cancel watch`, `store/begin-write attempt` and `store/begin-write rollback`."
  - "The goroutine sweep matched ten lines of text after each `go`, so a comment could hide a real recover (`internal/plan/plan.go`'s spine opener was reported bare) and a `go` that did not open its line was never read (`chatv3_sweep.go`). It parses the file and reads the goroutine's opening defers now; `TestEveryGoroutineInTheGuardedTreeIsGuarded` is out of `.github/known-red.txt` and off the list in CLAUDE.md."
  - "A goroutine passed the sweep only if its recover sat within ten text lines of the `go`. The rule is now that the recover is deferred before the first statement that makes a call: it may follow other defers, comments of any length, and call-free set-up such as a flag it will read."
  - "The recover the sweep accepts must be called at the TOP LEVEL of the deferred literal — a statement of its own, the right-hand side of an assignment, or the init or condition of a top-level `if`, which is the house shape `if r := recover(); r != nil`. A recover in a block below that (`if false { recover() }`, `for { recover() }`) is one the runtime may never reach and no longer counts. `switch r := recover(); r {` counts, on the same footing as the `if`."
  - "A goroutine somebody waits on delivers its result from a defer. The quorum verifiers and the write-lock transaction attempt sent from their happy paths, so wrapping them in `guard.Go` turned a crash into a wait with no end: a fallen verifier left `quorumVerify` reading a channel nothing would write, and a fallen attempt left its caller told `ErrBusy` and one rollback goroutine parked per fault. Both send from a defer now — a verifier that fell says no with the reason `REJECT: the check did not finish`, and a fallen attempt is reported as itself rather than as a busy lock."
---

The old sweep read text, and text lies in both directions: a recover behind a
paragraph of prose was invisible, and the word `recover()` in a comment would have
passed a goroutine that had none. Reading the syntax makes the rule the one the
runtime actually applies — a deferred recover protects everything registered after
it — and that is what let the one honest false positive stop being reordered around
and the one hidden bare spawn be found.

And a goroutine that is wrapped is not thereby made safe. `guard.Go` stops a panic
killing the surface; it does nothing for whoever is waiting on a result the panic
skipped past. Where somebody waits, the result now leaves from a defer, which is
the only place a recovered panic can still reach them.
