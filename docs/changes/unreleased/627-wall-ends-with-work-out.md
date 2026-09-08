---
kind: fixed
title: the wall is read while a session is idle, so hours that run out with work still going end the run
pr: 627
surface: [chat, engine]
invalidates:
  - "`Budget.Exhausted` had exactly one caller in the build — `Steward.Decide`, reached only at the end of a turn — so an unattended session that handed its work to a task and went idle never asked whether its hours were up, and `--max-hours` was in practice a ceiling only on a session that kept stopping to speak. Three canary cells handed over one to two minutes before their own wall and sat at `1 running` until the rig killed them at 902 seconds inside an 840-second wall, with no line on screen, no row after the ceiling row in the store and no ending. A session whose budget names a wall now arms one reader off the goal owner's own clock, and past the wall, with work still moving and no turn speaking, it ends the run through the same `stopSpent` the money ceiling takes."
  - "The ceiling's own stop was described everywhere — in `internal/manual/chat/starting-aforge.md` and in the code — as the one stop that ends a turn over live work and leaves that work exactly where it was: `· work was still going and was left where it was`. That is still the whole truth of a MONEY ceiling. It is no longer the truth of the hours: when the wall reader ends a run it stops every unsettled unit of work the way a person's stop stops one, so the row settles rather than going on claiming to run, and its line says so — `· work was still going, so it was stopped and what it did was kept`. Its branch, its working copy and everything it wrote are untouched; nothing merges and nothing is deleted."
  - "A landing that came home to an idle unattended session woke a turn whatever the budget said, and the graph's frontier started queued work on the same terms, so a run past its wall could still buy model calls and still start work it could not pay for. Neither happens now: past the wall `Agent.wakeLocked` starts no turn nobody asked for, and the wall's ending closes the frontier — which is why `TaskGraph.quitting` is no longer set only by the session's quit."
  - "A task's checkpoint interval was a flat 60 minutes whatever the run around it had left, so a node started inside a fourteen-minute wall was held to a leash four times longer than the run it belonged to and its own second look could never fall inside it. A node started by a session whose budget names a wall now gets `min(taskDeadline, what is left of the wall)`, floored at `taskAllowance` — the same setup-and-check margin the handover seam already refuses to hand over inside, rather than a new number."
---

Every reading of "are the hours up" this side of the engine was taken at the end
of a turn, and the shape that needed it most is the shape that has no turn
ending: a session that has handed its work out and is watching. The reader is
the FLOOR and not a second policy — a session with nothing moving still ends
exactly where it did, a turn that is speaking still reaches its own ending
rather than being sealed from outside, and a session somebody is sitting in
front of arms nothing at all.
