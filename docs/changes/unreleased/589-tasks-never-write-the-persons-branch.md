---
kind: changed
title: a task never writes a protected branch, and repository work is always on a branch of its own
pr: 589
surface: [engine, chat]
invalidates:
  - "`where: in place` on a proposal used to put the task's work straight into the person's checkout whenever the model asked for it; inside a repository with a commit the work now goes on a task branch, and the door says so once."
  - "`taskTree.landsInThePersonsRepository` answered false the moment `root` and `ground` differed, and `Agent.Land` builds its tree with `ground: tree.Folder` and `root: tree.Root` — so a ground one directory inside the person's checkout skipped every protection and merged onto the branch they were standing on. Nothing reached it, because `ReferPlace` and `groundRoot` both snap to the repository root; a ground WITHIN the root is read as the person's now regardless, and `TestC15ARepositorySubdirectoryGroundIsStillProtected` pins it where the guard relies on it rather than where the snap happens to enforce it."
  - "A landing used to merge the task's branch into whatever branch the root checkout happened to be on, `dev` and `main` included; a checkout on a protected branch, one that is on a DIFFERENT BRANCH than it was when the work was cut, or a detached one now keeps the branch instead and the report says which. The check is the branch NAME and not a commit: `home` records `currentBranch(root)`, so committing or rebasing on the same branch you started on is not \"moved\" and still merges, exactly as it always did."
  - "A finished task's merge word was one of merged, conflicted, inplace or aborted; there is a fifth, `kept`, for finished work deliberately left on its branch, and the surface draws it as `branch kept · task/x`."
  - "A task that named a prerequisite in `depends_on` used to receive only its report; when that work was kept on a branch, the dependent is now cut from that branch's tip, and several kept prerequisites are merged before its worker starts."
  - "`prepareTaskTreeAt` read `where` on its own; both placement roads now read one function, `whereInsideRepository`, so they cannot disagree."
---

Two roads put a task's work into the person's live checkout. The chat model set
`where: "in place"` on four code tasks in one session and `resolveTaskGround` took any
`where` as the person's own placement, skipping the ground ladder; and a landed branch was
merged into whatever branch the root checkout was on, which fast-forwarded a local `dev`
with an `aforge`-authored commit carrying a scratch file. The owner's ruling: a task never
writes a trunk branch, because other tools and people interact with it, and repository
work is always isolated on a branch of its own.

A model's `where` is now evidence, not authority, inside a repository. The one channel
that still puts repository work in place is the person's own word on a referred place.
Every worktree cut records the branch the root was on (`home`, additive on the task record
and the standing tree), and `comeHome` keeps the branch when the root is on `main`,
`master`, `dev`, `develop`, `development`, `staging`, `stage`, `trunk`, `production`,
`prod`, `release`, or any remote's default branch, is on a different branch than when
the work was cut, or is detached. Protection applies only where the root is the person's own repository: family
parts, mirrored family trees, legacy task folders and the owned workspace are exempt, so
nothing about families changes.

**The one thing "never writes your checkout" does not cover:** `hideFurrowMarker`
(`groundladder.go`) appends aforge's marker to the repository's `.git/info/exclude` while
grounding. It is metadata rather than tracked content and it predates this change, but it
is a write to the person's repository and the sentence should not be read as absolute.

Left for follow-ups, each with its own design: design nodes and saved subharness runs
still run in the session workspace with the full belt; fork hands share their parent's
directory; the chat's own inline-edit allowance and a cross-session write lease; a sweep of
kept `task/*` branches.
