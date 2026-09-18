# What a --yolo task turn waited on when a bash job backgrounded

Diagnosis of one recorded run: a `/task` worker turn under `--yolo` made 22
provider calls ($1.41) and then made none for 53 minutes, until the session was
closed from outside. No source change accompanies this note; it names the wait
from the run's records and from the code, and says whether the wait still exists
on `6e7d0ed44`.

## ANSWER

The turn was not waiting on the provider and not inside a subprocess wait: it
was parked, by design, in [`Agent.parkOnOwedJob`]
(`internal/session/task_job_park.go:90`) — the worker's step-boundary wait on a
foreground `bash` command that the session's background-after clock had promoted
into a job. The tool had answered `still running as job 5; log at …` (the
promotion marks the adopted command *owed*: `internal/session/jobs.go:235`,
adoption at `internal/session/jobs.go:917`), and a task worker does not ask the
model what to do next while a command it started is still running — it waits for
that command's ending and reads the ending as the answer. The wait selects on
three doors only (`task_job_park.go:114`-119): the task-news channel that a job
ending wakes, the turn context, and a one-shot timer armed with the node's whole
allowance (`time.NewTimer(bound)`, `task_job_park.go:104`; the bound is
`limits.deadline` from `internal/session/task_child_run.go:141`, defaulted at
`task_child_run.go:131` to `taskDeadline` = **60 minutes**,
`internal/session/task_run.go:106`). For 53 minutes none of the three fired: the
promoted command — a 24-count pinned re-run of the issue's own test, which the
worker had starved on purpose with CPU-pinned busy loops and a load loop — never
exited, so no ending note was ever posted; no steering existed; and the
allowance timer had not expired when the session was closed from outside. The
close cancelled the turn context with the stop door `session closed`
(`internal/session/agent.go:2283`), the park's `ctx.Done()` fired, and the turn
ended there — which is the journal's last worker-lane row. No goroutine was
leaked and no lock was held; the turn goroutine sat in the select, S state, at
under 1% subtree CPU, while background tickers kept the files warm.

## THE EVIDENCE

The record trail, in order (times local):

1. **The last provider call** is a `finish` row at 05:14:47 — status 200,
   `finish: "tool_calls"`, 46 messages, `hazard_ceiling_ms 10000`. There is no
   `start` row after it anywhere in the call log: the next request was never
   begun.
2. **The tool call did get a recorded result.** The task journal's last
   worker-lane rows are the assistant `tool_calls` naming `bash` (a pinned,
   24-count re-run of the issue's test, output redirected to a scratch file, a
   model-set 500-second timeout), then a `took` row of exactly **30000 ms** and
   the tool result at 05:15:17: `still running as job 5; log at …/jobs/5.log`,
   with `[job 3] running 3m12s`, `[job 4] running 2m10s`, `[job 5] running 0s`.
   The 30 seconds is the session's background-after clock
   (`internal/config/bash.go:8`, `DefaultBashBackgroundAfter = 30`) promoting
   the still-running call into an owed job (`internal/session/promote.go:165`).
   `jobs/5.log` is 0 bytes because the command's own output went to its scratch
   file, which shows 5 of 24 runs done within seconds of the start and nothing
   after — the worker's own load had starved the run it was waiting on.
3. **Then silence.** From 05:15:17 to 06:08:21 the worker lane writes nothing
   and no provider call begins; the turn's own accounting later stamps the gap
   (`stepGapMs 3184145` ≈ 53.1 min). Only tickers moved: the lane-status log to
   06:03:39, job 3's load loop to 06:04:29 (its tenth pass), presence to 06:08:19
   — still carrying the stale activity `thinking after bash … · 27 steps so far`
   — and the beat to 06:08:21.
4. **The end is the close, not a result.** The next worker-lane row is the
   error: `door: "session closed"`, `turn ended: session closed`, stamped
   06:08:21 — the instant of the external close. `Agent.Close` cancels the turn
   with `stopFor(StopByClosing)` (`internal/session/agent.go:2283`); the error
   row is written by the journal's failure path
   (`internal/session/sessionfile.go:3001`, `appendError`), carrying the
   would-be request's input estimate (69540).

Code path, from the tool response to the wait (line numbers for the run's
binary, `5d918064`; current-tree numbers beside where they moved):

- After a tool result is recorded, the loop reaches its step boundary:
  `a.parkOnOwedJob(ctx)` — loop.go **:863** in the run's binary, **:927** on
  `6e7d0ed44` — immediately before `drainSteering`, after the (non-blocking)
  checkpoint settle.
- The park itself: `internal/session/task_job_park.go` — early out at **:101**
  unless a job is owed and running (`jobRegistry.owedRunning`,
  `internal/session/jobs.go:826`), the allowance timer at **:104**, the select
  on news / `ctx.Done()` / `timer.C` at **:114**-119. Releases besides the
  ending note: a `jobs kill` and a stop release through the registry's paid
  hook (`internal/session/agent.go:192`, `internal/session/jobstop.go:90`).
- The bound: armed by the runner — `child.armJobPark(limits.deadline)`
  (`internal/session/task_child_run.go:141`), defaulted to `taskDeadline` = 60
  minutes (`internal/session/task_run.go:106`); a node's production limits
  always take `deadline: taskDeadline` (`internal/session/task_run.go:2690`).
- Why the model's own 500-second timeout did not end the job: once adopted the
  call's outcome is `bashAdopted`, and a timer that fires later is told not to
  kill (`internal/exec/bare/promote.go:194`, `closeTimedOut` returns false when
  the call was adopted). The process kept running until the session closed and
  the registry's shutdown killed it.
- **Which bound bounds what.** A hung *provider call* is bounded per call by
  the hazard ceiling (10000 ms in this run's rows) with retries after it. A
  hung *tool* is bounded by none of that machinery. Inside the turn the only
  bound is the park's allowance timer — one hour by default. Outside the turn
  the runner watches the same allowance with a timer of its own, kept for
  exactly this case (`internal/session/task_child_run.go:299`, armed at `:323`,
  read at `:327`), and it fires FIRST, because it is armed at the node's start
  rather than at the park — but what it opens is a reader's checkpoint and not a
  cut, which DOES IT EXIST ON THIS TREE takes up. No steward, watch-dog or step
  counter cuts a parked worker sooner, and by design none should: a park spends
  no step, books no meter and moves no clock (`task_job_park.go`, the header's
  third section).

No stack was captured at the time, so "the goroutine sat in the select" is a
conclusion from the records rather than a sample — but the records pin it: the
tool result was the last worker-lane write, no provider request ever began
behind it, the error row carries the close's stop door stamped at the close's
instant, and the park is the only blocking primitive between those two points.

## DOES IT EXIST ON THIS TREE

Yes. On `6e7d0ed44` the wait is present and unchanged, and the identity is
byte-level rather than impressionistic. The pinned commit
(`5d918064ff993b434aa3ad6b2e50e74e470917d1`) is NOT an object in this
repository — it is read from the build's own source store — but read there,
every file that carries the wait pipes identical to this tree's copy:
`internal/session/task_job_park.go` (the timer and the select),
`internal/session/task_child_run.go` (the arming at `:141` *and* the runner's
deadline drain), `internal/session/jobs.go` (`owedRunning`),
`internal/session/promote.go`, `internal/config/bash.go` (the 30-second
background-after clock) and `internal/exec/bare/promote.go` (an adopted call's
timer, told not to kill). Only `loop.go`, `task_run.go` and `agent.go` differ at
all, and the anchors cited above did not move inside them: `taskDeadline` is
still 60 minutes at `internal/session/task_run.go:106`, and the call site moved
from `loop.go:863` to `loop.go:927` because #1183 is the ONLY commit that
touched that file in between, and it added 64 lines above the call — the wait
did not move. So the same shape stands: a `/task` worker under `--yolo` whose
promoted foreground command never exits will sit idle — no provider call, no
lane write, S state — for up to the whole allowance, released early only by an
ending note, a `jobs kill`, steering, or a stop.

WHAT THE RUN WAS CLOSED 22 SECONDS SHORT OF WAS NOT A STOP. The node started at
05:08:43.6 and was closed at 06:08:21.9 — 21.6 s before its own 60-minute
deadline, measured from the node's start and not from the park. That deadline is
watched by a timer the runner keeps for precisely this silence ("a silent
command, parked job or model request may never produce the event that used to
notice this deadline", `internal/session/task_child_run.go:299`), armed at `:323`
and read at `:327`. When it fires it opens a CHECKPOINT: `r.checkpoint("deadline
checkpoint", true)` (`:329`) hands the run to a reader (`owner.taskProgress`,
`:267`), and a reader that says the work is still going pushes the deadline out
by another whole allowance (`:279`) — `taskMaxExtensions = 4` renewals, which
with the original allowance is five equal budgets, five hours by default
(`internal/session/task_run.go:163`-165). Only a not-working ruling, or four
spent renewals, sets `r.stopped` and cancels the run context (`:286`), and it is
that cancel which would have reached the park's `ctx.Done()`. Behind it stood
the park's own backstop, armed fresh at the park — 05:15:17.8 + 60 min ≈
06:15:17.8 — which ends the wait and lets the loop ask the model again. So had
the close not come, the silence would have broken at ~06:08:43 with a reader
being asked whether a node whose command had not ended was still working, and,
had that reader carried the node on, at ~06:15:17 with the park's timer. It would
not have broken with a stop at 06:08:43, and the run was not 22 seconds from
ending on its own.

## REPRO

Possible in well under an hour, and it was written and run once during this
diagnosis — it passes on `6e7d0ed44`
(`go test -count=1 -run 'TestAWorkerParkedOnANeverEndingCommandIsSilentUntilItsAllowance' ./internal/session`
→ `ok ... 3.065s`) — and was then removed again so this branch stays the one
file. It is a mechanism repro, not a failing test: the current tree *bounds*
the wait at the allowance, so what it pins is the wait and its only cut. A
worker runs a foreground command built from the file's own `holdACommand`
harness whose release gate is never written — a command with no ending — the
background-after clock promotes it (`jobNest` winds the clock to one second),
and the run is given a 3-second allowance. The assertions: the run ends (the
turn is bounded, not hung); between the promotion and the cut the worker asks
the model nothing (on a tree without the park, the second ask comes the instant
the promotion answers); and the run spends its allowance rather than ending
early.

```go
func TestAWorkerParkedOnANeverEndingCommandIsSilentUntilItsAllowance(t *testing.T) {
	record := &askLog{}
	// The gate is never written, so the command has no ending of its own — the
	// shape of a build that outlives everyone's patience. Nothing in this test
	// ever releases it: the only clock on the run is the allowance it is given.
	long := holdACommand(t, "the long one", "never")
	completer := &scriptedCompleter{steps: []step{
		bashStep(record, "the-long-one", long.text),
		sayStep(record, "the allowance is spent; here is where I got to"),
	}}
	here := jobNest(t, completer)

	started := time.Now()
	done, _ := runParent(t, here, taskLimits{maxSteps: 200, noProgress: 6, deadline: 3 * time.Second})
	waitPromoted(t, here.node)

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the worker never came back from its promoted command: the park outlived the run's allowance")
	}
	if ask2, made := record.askedAt(2); made && ask2.Sub(started) < 2*time.Second {
		t.Fatalf("the worker was asked again %v in, want it held silent until the allowance cut the turn", ask2.Sub(started))
	}
	if elapsed := time.Since(started); elapsed < 2500*time.Millisecond {
		t.Fatalf("the run ended %v in, want it to spend its allowance parked on the command", elapsed)
	}
}
```

One thing the repro taught that the records alone do not say: the allowance
stop *ends the turn* rather than asking the model a second question — the run
finishes with the single ask and a stop, the same end-shape the recorded run's
journal shows. The park's own timer and the runner's deadline are the same
number taken at different starts, so on a long enough run the runner's deadline
fires first — and what it fires is a reader's checkpoint that may renew the
allowance rather than a stop. The park's timer is the backstop behind it, and the
only one of the two that resumes the turn by itself.

## COVERED BY #1183/#1174?

Neither. #1183 (`360352ad`, "a settle turn runs under the checker's own bound,
not the run's wall") bounds the *settle* turn — the turn a landing note wakes
under `task.settle=auto` — and this wedge is the worker's own working turn,
woken by nobody. #1174 (`ad8be1ed`) only changes what a `--yolo` landing card
does with its default instead of parking the run on the card; no landing card
was involved here. The job park's bound remains the run's whole allowance on
this tree.
