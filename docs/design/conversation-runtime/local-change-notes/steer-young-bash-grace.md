---
kind: fixed
title: A correction typed into a young bash no longer waits for the command or the clock
surface: [chat, build, docs]
invalidates:
  - "A steer that found every foreground bash younger than 3 seconds was measured once and never again, so the correction waited for the command's own ending — or, in a live chat, for the 30-second background-after clock. The age is now re-read once, at the moment the youngest running call crosses the same 3 seconds, through the one adoption path the steer itself uses."
  - "`waiting for the running step` implied a wait of unknown length for a bash call. It is now bounded at about three seconds; the clause is unchanged because it was true when it was sent, and the transcript's own record of the correction is updated to say which of the two landings happened."
---

Local implementation note; no pull request has been opened.

## What was wrong

`Agent.Steer` → `steerRunningBashLocked` skipped every foreground call younger
than `steerBashAge` (3s) and scheduled nothing. The only other road to a step
boundary was `bashPromotion.Started`'s background-after clock —
`DefaultBashBackgroundAfter`, 30 seconds in livechat — or the command's own end.
So a side question typed 0.5s into a 60-second command reached the model at ~30
seconds. An earlier live calibration answering a side question at ~34 seconds is
consistent with this, though that run also failed on quality and cannot on its
own separate the two causes.

Measured on this branch, `internal/session`, with the background clock set to 20s
and a `sleep 7` foreground command, steering ~0ms after the call started:

| | when the model received the correction |
|---|---|
| before | 7.03s — the command's own ending (the 20s clock never fired) |
| after | 3.03s — `steerBashAge` plus a 10ms margin |

## The mechanism

`internal/session/steer_grace.go`. One `time.AfterFunc` per steer at most, armed
under `a.mu` for the instant the LAST call in the batch ripens, replacing (and
stopping) any watch already there. On firing it re-checks, under `a.mu`, the
identities it captured — `turnSeq`, the exact `*bare.BashCall` pointers, and a
freshly read steering queue — and then walks `steerRunningBashLocked`, which now
takes its calls as an argument so the watch can restrict it to what it armed for.
There is no second adoption rule, no second tool sentence and no second kind of
job.

- A stop phrase anywhere in the pending queue takes the stop arm even when a
  later sentence was typed after it: a stop must not become a healthy background
  job.
- The watch is stopped by the turn's cleanup, by `Abandon` and by `Close`; a
  firing that arrives anyway is inert on the identity checks.
- The firing goroutine takes `a.mu` directly and calls the job registry with it
  held, exactly as `Steer` does — it is never reached through a registry
  callback, which is the lock order that deadlocked a previous attempt.

## Tests

`internal/session/steer_grace_test.go`: the bounded landing with the process
adopted once and its exit still arriving; a quick command left alone with an
inert late timer; two steers sharing one watch with the stop winning; the
armed-for identities (wrong turn, foreign calls, a repeat firing); and an
interrupted turn leaving no job and no consumed steer.

## Limits

- The `EventSteerAccepted` clause a person already read is not rewritten, because
  the surface folds a repeat acceptance into the row it already drew
  (`internal/tui3`'s `app.steerAccepted`). Only the journal's `SteerMark` is
  corrected. A surface that wanted the live clause to change would need an event
  of its own.
- The bound is `steerBashAge`, so a correction can still wait up to ~3 seconds
  behind a bash call; short non-bash tools are unchanged and still finish first.
- No live-model run was made from this lane.
