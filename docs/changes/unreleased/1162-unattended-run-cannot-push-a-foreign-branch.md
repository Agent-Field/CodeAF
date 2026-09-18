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
`Agent.branchesThisRunCut`; the branch the run is standing on is read off its own
workspace (`currentBranch(config.Workspace)`, task_branch_protection.go). The
branch the checkout stood on when the run began is not among the cut set, which is
what covers `santos/dev` without growing `protectedBranchNames`
(task_branch_protection.go) into a name-containment test.

THE TEST'S SHAPE, AND WHY IT CHANGED. `TestAYoloRunDoesNotPushToABranchItDidNotCreate`
was written before the fix, in a posture that could no longer reach the closed road:
it set `Config.Interactive` and the `--yolo` policy but NOT `Config.Unattended`, so
the guard never saw an unattended run, and it gave the session no workspace, so
there was no branch to be wrong about. It now records the two facts the incident
launch really carried — `Config.Unattended` (the door's `cfg.Unattended = opts.Yolo`)
and a `Config.Workspace` standing on a real repository checked out on `santos/dev` —
and sends the same three verbatim lines down the same `ep.preAction` chain. Nothing
was weakened: the three lines, the chain and the assertion are unchanged; only the
posture and the branch are now the incident's. A companion test,
`TestAnUnattendedRunMayMoveOnlyTheBranchItCut`, covers the three scope lines apart —
a run's own branch (standing copy or task branch) still pushable, an attended
session untouched, and a foreign branch's push, merge and rebase refused while its
reading, saving and branch-creating stay its own.

THE ALTERNATIVE IT REJECTS. A `git push` entry on the critical-command floor
(`internal/approval/bash.go`) would also be consulted before an allow
short-circuits consent. It loses on two counts: it would refuse an INTERACTIVE
session's own push too — the act taskgit.go's header calls deliberately the
person's — and it matches command TEXT, so it can say nothing about which branch a
push would move, which is the whole question here.

WHAT THIS DOES NOT COVER. The resident's CONSEQUENCE classifier
(`internal/head/head.go:1123`'s `consequenceGated`, consulted from the resident's
own tool road at `internal/head/bash.go:214`) is the v1 resident's road:
`internal/session` imports `internal/head` in no non-test file, so that classifier
never sees a chat door's tool call, and this fix does not wire it up. The chat
road's own verb reading — `firstBranchMover`, added here — reads push, merge and
rebase and nothing about how much a command matters, which is a different question
from the one that classifier answers. Wiring the resident's classifier to the chat
road is another product's work and is left alone.
