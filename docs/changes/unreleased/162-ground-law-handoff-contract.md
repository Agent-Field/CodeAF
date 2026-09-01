---
kind: changed
title: a task starts from its parent's folder as it stands, and its brief is checked against it first
pr: 162
surface: [engine, chat]
invalidates:
  - "A task's working copy was `git worktree add … HEAD`, and `cutTaskWorktree` said so: \"WHAT THE BRANCH CARRIES IS HEAD AND NOTHING ELSE\". It is now cut from a machine commit of the ground AS IT STANDS — uncommitted edits and untracked files included — so a child handed out by a parent holding unfinished work gets that work."
  - "There was no ladder: a repository got a worktree and everything else got a copy. There is one, `internal/session/groundladder.go`, walked highest rung first — a furrow universe, a machine snapshot, a folder copy — and every task records which rung made its world and the string that names it (`TaskNode.Rung`, `TaskNode.Seal`, and the `its world is …` line in its log)."
  - "`internal/furrow` could only run a command inside a universe (`RunInFork`), and standingtree.go's header said a Fork door did not exist. It does: `Workspace.Fork` hands a universe back, `Workspace.DropFork` forgets one, and `furrow.Attach` attaches a folder aforge is about to fork rather than waiting for somebody to run `furrow watch`."
  - "The task start note read `your unsaved edits stay here · the task works from the last commit` and the proposal card read `from HEAD — unsaved edits not included`. Both said the opposite of what is now true: they read `your unsaved edits go with it · your own copy is untouched` and `from your folder as it stands — unsaved edits included`."
  - "The manual said a task's checkout is cut from your last commit, that new files you have never committed are invisible to it, and that \"there is no flag that sends a dirty working copy\". All three are gone; the page now says what travels, how (a commit called `the world this task started from: <title>`, which never comes home), and the one thing that still does not — anything `.gitignore` covers."
  - "A brief was words about state and the ground was state, with nothing binding them. `divide_work` now takes an optional `expects` per part — a place, optionally text that must be findable in it, and whether it must be there or must not — and the worker's folder is checked against it before the first model call. `propose_task` does NOT take one, because the fixed-prefix budget had no room."
  - "`TaskEnding` had seven values. It has eight: `stale`, for work that never started because its brief and its world disagreed. Its row on the surface reads `its world did not match`, and its report names every unmet assumption and what the parent can do about it."
---

Two halves of one seam, from the 2026-08-31 dogfood run. A parent task carved
five children while its whole implementation was uncommitted, so not one of them
got the world its brief described: four lanes were doomed at birth, and about $14
and an hour went on proving the ground was stale. One worker spent twenty-two
minutes and $7.99 rewriting a test file for a component its own brief said had
been deleted.

The ladder fixes the world. The manifest fixes what nobody could check about it:
a mismatch is now a directory walk and a landing that names what was missing,
rather than a loop nobody can read afterwards.

**The ladder chooses the world; it never changes the promise.** A repository
ground was promised a branch, so it takes the snapshot rung and keeps its
worktree, its branch and its merge — a furrow universe is a repository of its own
and grounding a repository task in one would turn every branch landing into a
copy landing behind everybody's back. So the universe rung reaches a ground that
was promised a copy, and a repository task still cannot inherit a `.env` or an
installed dependency tree. Closing that needs a landing road that can merge from
a separate repository, which is a decision about promises rather than grounds.
