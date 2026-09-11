---
kind: fixed
title: A task's checker is told what was already red, and old red no longer refuses a landing
pr: 611
surface: [chat, engine]
invalidates:
  - "A task's checker judged the acceptance against the tree exactly as it stood and had no reading of what the tree was already failing, so an acceptance saying `the test suite passes` was refused by ANY red, whoever caused it — two sub-tasks of the attrs cell did correct work, were both refused over tests that fail on the untouched checkout, and the run walled at `1 running · 2 needs you` with the fix sitting in a branch nobody brought home. The door's checks are now read on the commit the node's tree was cut from before the checker is asked anything, a check that was red on both sides is named to it as `already failing before this work began` and not this work's to answer for, and a check that was green and is now red is named as a finding. The session's own end-of-run reading has had this law since #513; a task's checker did not."
  - "The sentence `1 check was already failing before this work and is not counted: <command>` was the SESSION's alone, said in a steward's brief at the end of a reply. A finished task's landing now carries it too — the same `alreadyRedSentence`, never a second spelling — under the checker's own evidence and never over it. It rides the two roads that finish (a landing that holds, and one taken as it stands on an unattended run) and neither of the two that do not: a refusal keeps its attention on what is missing, and so does a landing that needs a person's look."
  - "The already-failing section of `starting-aforge` ended `A session you are sitting in front of runs none of this`. That is still true of the SESSION's own end-of-reply reading, which is taken only under a budget, and it is now false of a task's checker: that before-reading is taken whether somebody is watching or away. The section says which reading it is about, and *How tasks run* carries the task one."
  - "Every temporary detached checkout the check machinery makes used to be spelled out where it was needed — the node's own `-check` restore had the `worktree add --detach`, the forced removal, the prune and the repository lock written in its own body, and the ground restore had a second copy of them. There is one road now (`detachedWorktree`), taken by the clean restore, the ground restore and the base reading alike, so the three cannot drift into different git machinery."
  - "A reading of the checks was something only `Agent.readBaseline` did, over the session's declared checks. `internal/session/task_baseline.go` now takes the same shape of reading over a task's door: one window for the whole set, a check that MOVED the tree discarded rather than trusted, a check that could not start counted in neither direction, and the answer for one commit and one command shared by every node cut from that commit — so the parts of a divided job pay for one reading between them rather than one each. Where a task stands on a plain folder there is no base commit to read, and nothing at all is claimed about earlier red."
---

The half of the law that was missing. Since #513 the session's own reading has
known that a check red before the work is the project's and not the run's; the
task node's checker never had that reading, so it did the one thing a checker
with no baseline can do — refuse the acceptance over somebody else's failing
test. The reading it needed was cheap and already sitting there: the node's work
is staged rather than committed, so its own branch IS the tree before the work,
and one detached checkout answers the question for every part cut from it.

The second reading, of what would ship, is taken only when the first found red
to subtract — a clean base has nothing to take away, and a needless second pass
over somebody's suite is the cost this whole shape was drawn around.

Review follow-up: the before-reading uses the immutable commit captured at the
worktree cut, persisted as the Git check base alongside the ground seal and machine base, instead of
resolving the worker's current branch tip. A worker may commit its edits, and a
new failure in that commit must never be called pre-existing. Older records
without a captured commit provide no baseline. The committed-failure regression
fails against the original PR and passes with this correction.
