---
kind: fixed
title: a working copy that holds work is kept, whoever did the work
pr: 899
surface: [chat, engine, docs]
invalidates:
  - "Saying \"work in this folder directly\" about a folder whose working copy had already been cut DESTROYED anything in that copy the belt had not written. The retire's guard was `StandingTree.Wrote`, whose one writer is `noteStandingWrite` — reached only from the belt's `write` and `edit` — so a file a shell command made, something a script left, or a change a task working in that copy committed all left it empty while the copy held real work, and `git worktree remove --force` then deleted it with no refusal and no sentence. Reproducible with no key and no test: choose a repository folder, ask for a shell command that writes a file into it, say \"actually work in that folder directly\". This shipped in #876 and was live for one afternoon."
  - "The guard is no longer \"the belt did not write\" but \"the road that made the copy says it holds nothing\". A worktree is removed WITHOUT `--force`, so the refusal is git's own — it refuses on a modified file, a staged one and an untracked one, which is every way a copy comes to hold something — and that is the same move #876 already made for the branch with `git branch -d`, now applied to both halves of one function. A plain folder has no git to ask and is compared with the folder it was copied from (`mirrorHoldsWork`): a path the folder does not have, or has at a different length, is work. `Wrote` remains only the cheap first answer and the doc says it is never the authority."
  - "`Agent.startStandingTree` is GONE, one release after #876 named it. The deferred pass behind a referred folder no longer means \"cut what is missing\" but \"the folders that may be copied have a copy, and the folders that may not do not\" — one pass, both directions — so the two doors that can move that answer (`ReferPlace`, `SetPlaceMode`) say only that it moved, through `Agent.standingTreesOwed`. `SetPlaceMode` no longer settles the cut and no longer retires anything itself: asking git whether a copy may go, or walking a mirror that has no git, is work of exactly the kind that must not happen on a person's path, and nothing they do next depends on it — the mode is recorded under the lock, so the very next write already goes where they said. A test that counts copies after `SetPlaceMode` must `SettleWrites()` first, which is the same contract every other deferred write in #876 carries."
  - "A refusal is not an error and is not announced. The copy is kept WHOLE — directory, branch and record — because dropping the record would take the `changes for <name> · /land` chip that leads the person back to the work still sitting in it. Only the folder written into from then on changes. The manual said the copy was kept when \"something has already been written into\" it; it now says work of any kind, names the shell command and the task, and says which authority is asked on each road."
---

`mirrorHoldsWork` has one blind spot and it is written down in its doc rather
than guarded against: an existing file edited to EXACTLY its old length. Seeing
that means reading both copies of every file in the folder, which is the whole
cost the copy was made to avoid and would be paid on a word a person says in
passing. Every answer the retire is unsure of — an unreadable copy, a walk that
fails — is "keep", because removal is the only destructive thing it does.

Residuals 2 and 3 of #897 (the check-then-remove window, and the pre-existing
unguarded `a.config` reads in the same files) are deliberately not folded in
here.
