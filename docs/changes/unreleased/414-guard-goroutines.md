---
kind: fixed
title: The goroutine sweep reads the defers, and every bare spawn in the guarded tree runs under guard.Go
pr: 414
surface: [chat, engine, build]
invalidates:
  - "Eight goroutines in the guarded tree (the quorum verifiers, the host follow pump, the once-per-launch home sweep, lanes.Persist, the bare executor's wait and cancel watchers, the write-lock transaction attempt and its abandoned rollback) ran without a recover and could take the terminal down. All run under `guard.Go` now, as `chat/quorum-verify`, `chatv3/host-follow`, `chatv3/sweep-home`, `lanes/persist`, `exec/bare wait`, `exec/bare cancel watch`, `store/begin-write attempt` and `store/begin-write rollback`."
  - "The goroutine sweep matched ten lines of text after each `go`, so a comment could hide a real recover (`internal/plan/plan.go`'s spine opener was reported bare) and a `go` that did not open its line was never read (`chatv3_sweep.go`). It parses the file and reads the goroutine's opening defers now; `TestEveryGoroutineInTheGuardedTreeIsGuarded` is out of `.github/known-red.txt` and off the list in CLAUDE.md."
  - "A goroutine passed the sweep only if its recover sat within ten text lines of the `go`. The rule is now that the recover is deferred before the first statement that makes a call: it may follow other defers, comments of any length, and call-free set-up such as a flag it will read."
---

The old sweep read text, and text lies in both directions: a recover behind a
paragraph of prose was invisible, and the word `recover()` in a comment would have
passed a goroutine that had none. Reading the syntax makes the rule the one the
runtime actually applies — a deferred recover protects everything registered after
it — and that is what let the one honest false positive stop being reordered around
and the one hidden bare spawn be found.
