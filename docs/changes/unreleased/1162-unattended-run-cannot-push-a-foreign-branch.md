---
kind: fixed
title: An unattended run cannot push or move a branch it did not create
pr: 1162
surface: [engine]
invalidates:
  - "An unattended `codeaf chat --yolo` run could push any branch from its own bash: `--yolo` made the consent default allow, so no card was drawn, and the git guard left the run's `[Person]` unguarded. A run standing on a branch it did not create is now refused `git push`, `git merge` and `git rebase`."
  - "The bash `git push` acts a task and a steward are refused were the whole of the git guard's reach. A third register (`refusedUnattendedBranchMovement`) now refuses an unattended run the three verbs that MOVE a branch it did not cut, while its reading, saving and branch-creating stay its own."
---

THE MECHANISM. The git guard (`internal/session/taskgit.go`) gains a third
register, `refusedUnattendedBranchMovement`, reached only when neither the task
gate nor the steward gate answered. A session that is an unattended run
(`Config.Unattended`, the door's record of `--yolo`) standing on a branch it did
not cut is refused `git push`, `git merge` and `git rebase` — the three acts that
SEND a branch out or REWRITE the commits it points at. Ownership is read from the
two records a cut makes: the conversation's standing copies
(`StandingTree.Branch`, standingtree.go's `cutStandingTree`) and the graph's task
branches (`TaskNode.branch`, task_run.go's `prepareTaskTree`), gathered by
`Agent.branchesThisRunCut`. The branch the checkout stood on when the run began is
not among them, which is what covers `santos/dev` without growing
`protectedBranchNames` (task_branch_protection.go) into a name-containment test.

THE ALTERNATIVE IT REJECTS. A `git push` entry on the critical-command floor
(`internal/approval/bash.go`) would also be consulted before an allow
short-circuits consent. It loses on two counts: it would refuse an INTERACTIVE
session's own push too — the act taskgit.go's header calls deliberately the
person's — and it matches command TEXT, so it can say nothing about which branch a
push would move, which is the whole question here.

WHAT THIS DOES NOT COVER. The high-stakes classifier at `internal/head/head.go`'s
`consequenceGated` is the v1 resident's road: `internal/session` imports
`internal/head` nowhere, so nothing on the chat road classifies a bash command by
verb. Wiring that up is a different product's work and is left alone.
