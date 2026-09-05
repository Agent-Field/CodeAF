---
kind: fixed
title: A correction typed into a young bash no longer waits for the command or the clock
surface: [chat, build, docs]
invalidates:
  - "A steer that found every foreground bash younger than 3 seconds was measured once and never again, so the correction waited for the command's own ending — or, in a live chat, for the 30-second background-after clock. The age is now re-read once, at the moment the youngest running call crosses the same 3 seconds, through the one adoption path the steer itself uses."
  - "`waiting for the running step` implied a wait of unknown length for a bash call. The HANDOFF is now bounded at about three seconds — not the model's reply, which still waits on the rest of the batch and on the request itself; the clause is unchanged because it was true when it was sent, and the transcript's own record of the correction is updated to say which of the two landings happened."
  - "An explicit stop phrase used to wait out the same 3-second grace as an ordinary correction when the command was young. It now reaches a foreground command at any age and cancels the armed watch, and a superseded stop left on the queue no longer holds authority over the delayed look."
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

An explicit `stop` typed at a half-second-old command: unbounded before (it
waited for the command), and 0.02s now, measured inside `Agent.Steer` itself.

These are figures for the HANDOFF — the moment the command is let go of and the
next request can carry the words. The model's reply is not bounded by them.

## The mechanism

`internal/session/steer_grace.go`. One `time.AfterFunc` per steer at most, armed
under `a.mu` for the instant the LAST call in the batch ripens, replacing (and
stopping) any watch already there. On firing it re-checks, under `a.mu`, the
identities it captured — `turnSeq`, the exact `*bare.BashCall` pointers, and a
freshly read steering queue — and then walks `steerRunningBashLocked`, which now
takes its calls as an argument so the watch can restrict it to what it armed for.
There is no second adoption rule, no second tool sentence and no second kind of
job.

- An explicit stop never enters this file: `steerRunningBashLocked` ignores the
  age gate for a stop phrase, so it reaches a command of any age the instant it
  is typed, and `Steer` cancels any armed watch on that road. The delayed look
  therefore acts for the LATEST still-pending correction only — a superseded
  stop is a directive the person moved on from, not standing authority, and
  both lines reach the model in order for it to decide.
- The watch is stopped by the turn's cleanup, by `Abandon` and by `Close`, and
  a firing whose watch the agent is no longer holding returns at once — a
  replacement inside one turn shares the turn number and the calls, so that is
  the only check that tells the two apart.
- The firing goroutine takes `a.mu` directly and calls the job registry with it
  held, exactly as `Steer` does — it is never reached through a registry
  callback, which is the lock order that deadlocked a previous attempt.

## Tests

`internal/session/steer_grace_test.go`: the bounded handoff with the process
adopted once and its exit still arriving; a quick command left alone with an
inert late timer; an explicit stop reaching a half-second-old command at once
and leaving no watch armed; the newest-direction rule including a superseded
stop; a watch replaced inside its own turn firing inertly; the armed-for
identities (wrong turn, foreign calls, a repeat firing); and an interrupted
turn leaving no job and no consumed steer.

## Root review, second pass

Two defects found in the first commit and fixed here:

1. `steerGraceFired` cleared `a.steerGrace` only when it matched and then acted
   regardless, so a watch replaced by a second steer inside the SAME turn could
   still adopt — the different-turn test could not see it. There is now an
   explicit return, with `TestAReplacedSteerGraceIsInertInItsOwnTurn` as the
   counterexample (it fails against the first commit with
   `a watch the agent had already let go of adopted a command`).
2. The delayed look took any queued stop phrase as standing authority. An
   explicit stop now acts immediately at any age instead, which removes the
   question rather than answering it; no classifier and no resume vocabulary
   were added.

A live functional probe (root, `grace-live-before/ADJUDICATION.md`) measured the
original accepted → consumed gap at 30.620791s, which is the background-after
clock and matches the mechanism above, separately from that run's wrong answer.

## Limits

- The `EventSteerAccepted` clause a person already read is not rewritten, because
  the surface folds a repeat acceptance into the row it already drew
  (`internal/tui3`'s `app.steerAccepted`). Only the journal's `SteerMark` is
  corrected. A surface that wanted the live clause to change would need an event
  of its own.
- The bound is `steerBashAge` plus scheduling, so a correction can still wait
  ~3 seconds behind a bash call; short non-bash tools are unchanged and still
  finish first. Nothing here bounds what the batch or the provider then cost.
- No live-model run was made from this lane.
