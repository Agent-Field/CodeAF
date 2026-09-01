---
kind: fixed
title: a task's card names where the work happened in plain words, from the rung that made it
pr: 198
surface: [chat, engine]
invalidates:
  - "The settled card labelled every task's directory `worktree` (`taskCardTreeWord`) and the landed card labelled its BRANCH row `worktree · ` (`doneBranchLabel`). Neither word exists any more: both rows are labelled from the rung that made the node's world — `its own copy of the folder`, `a branch of your repository`, `your own folder` — and the landed card falls back to `branch · ` when nothing recorded the rung."
  - "`GroundRung` was a record with no screen words behind it, so a surface that wanted to name a task's directory had to invent one, and internal/tui3 had. There is one table and it is the engine's: `session.GroundWord(rung, promise)` in `groundladder.go`, keyed by the rung and falling back to the promise for a record row written before the ladder. `taskTree.world()` is unchanged and is the long form, for the node's own log."
  - "`TaskNotice` and `TaskIndexEntry` carried the ground and the promise and not the rung, so no surface could tell a worktree from a fork. Both carry `Rung` now (`groundRung` on the record row, additive, absence unknown)."
  - "An interrupted task's report read `paused — it resumes; branch task/… kept, its worktree is at <dir>`. It reads `, its working copy is at <dir>` — the sentence beside it already called it a working copy, and after #183 the directory is often a fork rather than anything git registered."
  - "Nine manual pages described a task's directory as a `worktree` in prose. They say `working copy` or `a copy of its own`; what still says `worktree` is only git itself — `git worktree list`, a failed `git worktree add`, the refused-verb table, the `.git`-is-a-file linked-worktree refusal, and the two sentences that describe both shapes on purpose."
---

Found by the #172 lane and left as #194: after #183 a repository task is
grounded in a furrow fork whenever furrow can make one, and the card went on
naming the mechanism it had named since before the ladder existed. A person
reading it was told about a git worktree the work had never gone near, in a word
this codebase bans in anything a person reads.

The fix is one table rather than one string: the rung is the only thing that
knows what a task's directory IS, so the words live beside it and both surfaces
that name the place — the settled card in the project's record, and the landing
note a task writes when it comes home — read them from there. Two surfaces
wording one fact separately is two wordings that drift, which is exactly how the
card and the ladder came to disagree in the first place.
