---
kind: fixed
title: a check run over an unchanged tree is not run again, and none starts that the wall cannot hold
pr: 633
surface: [chat, engine]
invalidates:
  - "The session's declared checks were executed from scratch by every reader of the same tree — once by the before-reading at session start, and again by the terminal reading at the END OF EVERY TURN whose goal owner read done, on both the stopped-turn road and the handover. Nothing reused anything. An answer is now remembered against `verify.TreeState` of the tree it was taken over, and a later reader over an unchanged tree is given that answer rather than paying for it again; a check that MOVED the tree is never remembered, because its answer is about a tree it changed itself, though how long it took is remembered either way so the next reader knows what the command costs. Measured (#546's canary, `Human-Agent-Society-reef-145` under `turnWallShare=3`): the work finished at 588 s with 315 s of a 903 s wall left, the project's suite takes about 150 s, and the run still ended `wall · tests green`."
  - "`Budget.Left()` — the remaining-wall reading in `internal/session/principal.go` — had NO CALLER anywhere in the repository, so no check anywhere knew how much of the wall was left. Every session-side window is now the smaller of its own bound and what is left on the goal owner's own clock: one check and the whole before-reading photograph off `sessionCheckWindow`, and `auditWindowFor` off the door's own window, floored at `auditReadingDeadline` because an audit can always judge from reading and a window of nothing is not a window. A session with no wall named — a person's, or a money ceiling alone — is unchanged and still gets the flat five minutes."
  - "A declared check was always started, however little of the wall was left, and a suite killed at the wall took the run down with it. A check is now started only while what is left can hold it: at least as long as the last run of that same command took where that is remembered, and at least `verify.ShortestUsefulReading` where it is not. One that does not fit is NOT EXECUTED, and it is said out loud — `finishing here · what was asked is done · unchecked: there was not enough time left to run tox -e py`, on both roads that can end an unattended run, with the commands on the journal's `checked` row as `unread`."
  - "Every check that came back not passing was red. `CheckRun` now carries `Unread`, the reason a check was never started, and `Remains.redChecks` skips one that has it — a check nobody started is neither a pass nor a failure, so it is not counted against the run and cannot by itself hold a finished ask open. An unchecked ending is honest; a wall ending over finished work is not."
  - "`internal/manual/chat/starting-aforge.md` said the run \"runs them again\" at the end. That is true only where the tree has moved under the answer already taken, and the page says so; the rule that only a check GREEN BEFORE AND RED AFTER counts as work still to do is unchanged and stands on its own. A new section answers the three questions people actually ask — why the same tests ran three times, why it ran out of time running them, and what `unchecked` means at the end of a run — and quotes the line exactly as the code spells it."
---

Three readers ran the same suite over the same tree and the two that can END a
run had neither a clock nor a memory. `internal/verify` already held both halves —
`TreeState`, which says a tree has not moved, and `ShortestUsefulReading`, the law
that a run whose wall cannot afford a reading takes no reading at all — and the
session package used neither. Nothing new is invented here; the two are wired to
the seam that was losing the wall.

**The node's own checker is deliberately not served by this memory.** Its check is
a model typing into a bash tool inside the node's ground restore, not this package
starting a process in the deliverable tree, and `TreeState` is size-and-modification-time
by its own stated law — so a restore and the tree after the merge cannot digest equal
even holding identical content. Content-addressing across those two roots would be a
hit that never fires wearing the clothes of a fix. That road wants a content digest
`internal/verify` does not have, and it is its own change.

Review follow-up: a cancelled or unstarted check no longer enters the memory of
answers or execution times. Before this correction, a cancelled reading poisoned
the next live reading of the unchanged tree with its cached non-answer. A
deterministic cancelled-context regression reproduces that failure.

This change still does not share a task checker's result across its restored
worktree and the final deliverable tree. That part of issue #609 needs a content
identity and a shared execution receipt; the current metadata photograph cannot
prove equality across those roots. The task audit also retains its existing
reading-time floor when the session wall has less time remaining.
