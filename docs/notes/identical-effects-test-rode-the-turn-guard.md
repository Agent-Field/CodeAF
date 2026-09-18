# The identical-effects test rode the turn guard, not a leash threshold

## The question, answered

`TestARunOfIdenticalEffectsEndsNothingByItself` (`internal/session/effects_test.go`)
failed once on its `len(rounds) < 2` assertion in a full-package run under load —
0.21s — and passed alone. The brief left two candidates open: the deadline branch
above the repeat branch in `childRun.trip`, and the turn's own repetition guard in
`looped.go`. **It is the second, and the deadline branch never fired.**

The threshold the run actually reaches is the child turn's own repetition guard:
`loopWatch.observe`'s identity rule (`internal/session/looped.go:446`, the
`case run >= loopRepeats && !ok && w.speakAbout(signature):` arm) tips, returns a
`nudge`, and the loop hands the turn over — the report the node lands with is
`this turn is going in circles · stopping here with anything remaining left
undone` (`loopLeftUndoneNote`), not any line from `trip`'s four cases.

The deadline branch (`task_child_run.go:594`, `case !r.now().Before(r.deadline):`)
could not have been the one: the run's allowance is `taskDeadline = 60 * time.Minute`
(`internal/session/task_run.go:106`), the test pins no clock (fact 4), and the
whole run lasted 0.21s. `trip`'s two count-driven cases could not fire either —
the step budget is 200 and only seven steps ran, and every save resets `r.idle`
(`task_child_run.go:562`, `case added: r.idle = 0`), so `r.idle >= 2` never
reaches.

## Why the reader end up asked fewer than twice

With forty identical `write` calls and no clock pinned, the child turn is ended by
the guard above at its **seventh** write, and the run's own drain
(`childRun.drain`, `task_child_run.go:298`) processes the tool events on a second
goroutine. The repeat checkpoints land on the third and fifth writes (and, when
the drain reaches the seventh before the turn closes, a third at the seventh). It
is a race between that drain and the turn end, and it already shows **without any
load**: instrumented on this box, `len(rounds)` came back 2 or 3 across 250 runs
of the untouched test, never 1. The assertion `>= 2` sat exactly on that boundary,
so the smallest further loss — the run losing the fifth write's checkpoint too —
took it to 1 and failed.

Reproducing the load-induced 1 is the one thing the brief forbids, so it could not
be observed directly. What reading settled is that the guard's own counters do not
depend on load: the batches are `write` calls, whose `materialProgress` returns on
`mutatedPath` (`looped.go:589`) before it ever asks the worktree, so the silent
ladder and the no-new-information streak cannot move with contention and the
hand-over stays at the seventh write. The drop to one ask is the drain-vs-turn
race above losing a second checkpoint, not a threshold moving.

## Is the reopen wrong?

No. The leash's repeat rule behaves exactly as documented: it raises
`repeat checkpoint`, the reader answers WORKING, and `checkpoint`'s
`working && !renew` arm (`task_child_run.go:268`) pardons the run so the next save
is asked about afresh. Nothing asks a real second round of identical effects
wrongly, and no deadline branch stole a checkpoint the repeat branch should have
had. The reopen was a test riding a race between the run's drain and the turn's
own guard — a real flake, but not a defect in the guarded behaviour.

## The fix

Two real-time dependencies, both removed from the test's construction
(`internal/session/effects_test.go`):

1. **The turn guard.** The forty saves now spell their JSON differently (the key
   order turns over) while leaving the same effect. `callSignature` keys on the
   call's tool and raw arguments, so no call is a call repeated and the guard
   never fires; `effectPrintOf` keys on what was left behind, so the effect is
   still one. The turn now runs all forty saves and ends on its own final line,
   where before it was ended by the guard at the seventh.

2. **The effect fingerprint race.** The saved content is empty, so the file is
   zero bytes before, during and after every save. A truncating write of nothing
   never changes the file, so `effectPrintOf`'s read of it
   (`fileEffectPrint`, `effects.go`) returns the same sum whether the drain gets
   to it before or after the next save. With any non-empty content that read races
   the next truncating `os.WriteFile` (`internal/exec/bare/tools.go:865`), the
   fingerprint flickers between the file and the write's own answer sentence, and
   `sameEffectRun` resets — a second, independent flicker that shifted the count
   by one.

The clock seams in fact 4 are not reachable from this test's construction.
`newTestAgent` returns the parent session; the node's worker is built by
`newTaskAgentOn` → `newChildAgent` → `newAgent(config, client)`
(`internal/session/agent.go:82`), which is a fresh `Agent` and inherits no
`taskNow`/`taskTimer` (those are `Agent` fields, not `Config`). The only door to
the child is `node.openRoom().speaker()` (`task_room.go:1060`), and it is set
mid-run and cleared at landing, so reaching it is itself a race — and it would pin
nothing here, because the deadline it would suppress is 60 minutes and cannot fire
in a test. So the nearest deterministic thing was pinned instead: the two real-time
dependencies that actually decided this test.

## It still bites

Removing `r.effects.pardon()` from `checkpoint`'s `working && !renew` arm
(`internal/session/task_child_run.go:273`) leaves the run never clearing, so every
save past the first run meets the same count and the reader is asked on every
step:

```
$ go test ./internal/session -run '^TestARunOfIdenticalEffectsEndsNothingByItself$' -count=1 -v
    effects_test.go:252: the reader was asked 38 times over 40 identical saves, want it asked once per 2-save run and never on every step (at most 21)
--- FAIL: TestARunOfIdenticalEffectsEndsNothingByItself (0.10s)
```

Reverted:

```
$ go test ./internal/session -run '^TestARunOfIdenticalEffectsEndsNothingByItself$' -count=5
ok  	github.com/Agent-Field/codeaf/internal/session	0.347s
```

The old bound (`>= 2`) did not bite: without the pardon the count rises to 38, and
2 is still `>= 2`. The bound now holds the behaviour the reset exists for — asked
once per `noProgress`-long run, never on every step — and the reader is asked
19 times over the forty saves, deterministically.

## What reading could not settle

The exact step at which load costs the second ask. The drop to one ask could not be
reproduced on this box without generating the machine load the brief forbids, so
the precise mechanism of the load-induced loss (the drain falling a checkpoint
further behind, rather than any counter moving) is inferred from the guard's own
load-independence and the drain-vs-turn race that already shows without load.
