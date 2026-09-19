# Attribution: what a waiting chat surface does with the CPU

Established before any change, on this tree (build at commit 39aee797,
`task/c286-idle-surface-cpu`), with the replay harness in this directory.
Method: read-only sampling of the per-thread stat fields of the surface
process under procfs, utime+stime differenced over 30s windows, inside
`unshare -rn --mount` with the loopback stub (bench/idle-surface/replay.sh,
wait.sh). No load was generated; nothing ran except the surface, the stub
and tmux.

## The measured states, by K

Settled idle (turn finished, nothing in flight, nothing sent) is the
before-curve from the replay harness (results.md): 0.80/0.87, 0.87/0.77,
1.07/0.80 percent of a core at K=5, 50, 200. Flat in K.

Waiting on a model, request sent, no first delta yet (spinner only, paint
clock running), from wait.sh (wait-20260918-202034.tsv and -202313.tsv):

| K | mode | %core w1 | %core w2 | RSS MB | threads |
| --- | --- | --- | --- | --- | --- |
| 5 | nodelta | 5.13 | 1.57* | 91.8 | 15 |
| 200 | nodelta | 6.40 | 2.30* | 101.7 | 16 |
| 5 | stream | 3.27 | 3.20 | 81.6 | 14 |
| 200 | stream | 6.23 | 5.67 | 101.3 | 16 |

Rows marked * are windows in which the held turn settled (the 90s stub hold
ran out mid-window); the honest steady figure for a held wait is w1 and the
stream pair, ~3 to 6 percent of a core.

Commands: `bash bench/idle-surface/wait.sh 5:nodelta 5:stream` and
`bash bench/idle-surface/wait.sh 200:nodelta 200:stream`. The stub's delay is
tunable mid-flight (bench/idle-surface/stub-tunable.py reads a delay file per
token), which is how one turn is held open on 5ms (normal), 1500ms (slow
stream) or 90000ms (no first delta) without touching the surface.

## What each percent IS, with its code path

1. **The paint clock while a turn is unresolved: ~2 to 5 points of a core.**
   internal/tui3/app.go:42 `frameInterval = 33ms`; the clock is
   `app.frameTick` (app.go:6151) rescheduled by `app.paint` (app.go:4706)
   and gated by the liveness predicate at app.go:4832-4926, whose first term
   is `a.state == stateWorking`. While any term holds, the surface builds a
   whole frame 30 times a second: spinner step, row layout, status row.
   Measured: the delta between settled idle (~1%) and a held wait (~6%) is
   this clock plus what a frame costs. Pinned by the wait.sh experiment
   above: the same K, the same profile, the only difference being a turn in
   flight.
2. **The pointer settle clock, dormant while idle.**
   internal/tui3/coalesce.go:67 `pointerEvery = frameInterval/2`, armed only
   by `app.pointerWake` after input (coalesce.go:224). It costs nothing while
   nobody types; it is named here because it is the second fastest clock on
   the surface and the experiment rules it out of the idle figure.
3. **The runtime's own idle pool: the floor, under one percent.**
   cmd/codeaf/main.go:76-93 records the measured bargain: idle GC workers
   held one per P until GOMAXPROCS was capped for surface commands. The
   settled ~1% floor, flat in K, is this remainder; it is not conversation
   work.
4. **NOT markdown re-parsing and NOT transcript re-rendering.** Settled
   entries are cached: internal/tui3/app.go:571 (row cache keyed on the
   facts that change painted rows) and internal/tui3/render.go:1242
   (`assistantRows`: the streaming tail is plain wrapped text; the settled
   prefix is promoted on a 1.5s throttle, app.go:50 `markdownThrottle`). The
   K-flatness of both the idle and the waiting figures is the measurement
   that pins this: K=200 costs what K=5 costs.
5. **NOT the engine daemon.** Under `--no-host` the engine runs in the
   surface's own process (cmd/codeaf/chatv3_local.go:130 spawns the daemon
   only on the hosted road). The field pids had zero direct children, so
   their whole figure was one process, and the harness reproduces exactly
   that shape. The engine's tickers are slow (takeover.go:81 250ms
   doorstep, taskpresence.go:124 5s heartbeat, jobbound.go:97 2s) and none
   of them showed in the figures.

## The field figures, and the residual that is honestly unknown

Nothing in this build's reachable surface states reproduces 25% or 120% of a
core. The states a waiting surface can hold were each measured: settled
idle ~1%, held no-delta wait ~6%, slow stream ~6%, at K=5 and K=200, with a
pinned profile and a stub endpoint. The two field burners (25% small-K,
120% large-K, eleven threads each at 9-16% of a core, steady, zero children,
no transcript write for 217s) are therefore NOT attributed to any consumer
in this tree's code as built here, and the residual is named UNKNOWN.

What the field facts allow and do not allow: fact 6 establishes that
pid 1355567 had a second agent started through its chat door working in the
same worktree, with uncommitted edits present. "A surface that was only
waiting" is not established for that pid. pid 2374565 (25%) had no such
event and is not explained either; it remains unknown. The even spread
across many threads, steady at 9-16% each, is the signature of neither the
paint clock (one thread drawing) nor any ticker in this tree (all slower
than 33ms and all cheap); a build without the GOMAXPROCS cap of
cmd/codeaf/main.go would hold more idle workers but idle workers do not
burn. The one branch this work cannot measure on the laptop (a task run in
flight under the surface, roomorch.go:448 `orchPollEvery` polling and the
rail's live tree, all of which draw on the paint clock while
`a.tasksAnimating()` holds) is the next place to look, on Spark, with the
same wait.sh harness and a live task instead of a live model.

## Answer to "what determines how much"

On this build: whether a turn or other liveness predicate holds the paint
clock, and nothing else measurably. Conversation size (K=5 to 200, transcript
to ~1.3 MB scale) moves the figure by at most a point or two. A waiting
surface that is settled is near idle at every K, which is why no change is
proposed from this side of the question; the narrow-change work belongs to
whoever can hold the unexplained field state in a harness.
