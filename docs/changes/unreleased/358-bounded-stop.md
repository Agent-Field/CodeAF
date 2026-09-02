---
kind: fixed
title: stopping is bounded at ten seconds, it says so on the way, and there is a door behind it
pr: 358
surface: [chat, engine]
invalidates:
  - "`stopping` was unbounded BY DESIGN and there was no second stage. app.go's own comment block said so at length under the heading WHY THERE IS NO SECOND STAGE, and internal/manual/chat/keys.md said `No key makes it stop harder, and there is no second stage`. Both are now wrong. The window is bounded at 10 seconds measured from the keypress, and past it the surface detaches."
  - "The reason given for having no second stage was that there would be nothing behind it — that `session.Agent.Interrupt` cancels an already-cancelled context and the long waits never look at a context at all. That was true and is no longer: the jobs registry's SIGTERM and SIGKILL graces take the call's context, bash's wait has a cancellation arm, and the tool batch's `wg.Wait()` has a second stage cut by an abandon signal. `session.Agent.Abandon` is the door onto all three."
  - "A turn that was never waited for left no record of it. There is now an `abandoned` line in the session journal carrying the reason and the turn's last known spend, and it is written exactly once per turn."
  - "The status line said only `stopping`. It now says `stopping · detaching in 7s` while there is a door behind the bound, and the bare word where there is not."
---

A person who stops a turn must be able to end it, and until this they could not. A turn
parked on a wait no cancellation reaches left `esc` inert — four minutes in the measured
run of #265 — and the only key left was `ctrl+c` twice, which takes the process and every
other conversation in it.

There is no new key, because there is no key left: `esc` within half a second belongs to
the rewind and `ctrl+c` is the interrupt mid-turn and the quit arm at rest. The second
stage is a CLOCK started by the `esc` already pressed, and the bound is on the screen
while it runs down — a countdown a person cannot see is a bound they have to take on
trust. Ten seconds sits above the longest ordinary letting-go (bash's three-second
`WaitDelay` on a leaked pipe, a `jobs` kill's two-plus-two-second graces), so the deadline
only ever fires on a turn that was genuinely not going to end.

What it does at the deadline is the part that matters. The waits actually end, the
in-flight request is actually aborted — every provider request is already built on the
turn's context, so no new transport machinery was needed — the surface lets go of the
stream, and one journal line records the turn as abandoned with what it had spent. The
money was never at risk: it reaches the ledger per call rather than per turn (#269). What
was missing was any record that a turn had been let go of at all.
