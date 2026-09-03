---
kind: changed
title: a task never writes a protected branch, and repository work is always on a branch of its own
pr: 589
surface: [engine, chat]
invalidates:
  - "`where: in place` on a proposal used to put the task's work straight into the person's checkout whenever the model asked for it; inside a repository with a commit the work now goes on a task branch, and the door says so once."
  - "A landing used to merge the task's branch into whatever branch the root checkout happened to be on, `dev` and `main` included; a checkout on a protected branch, one that moved since the cut, or a detached one now keeps the branch instead and the report says which."
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
`prod`, `release`, or any remote's default branch, has moved since the cut, or is
detached. Protection applies only where the root is the person's own repository: family
parts, mirrored family trees, legacy task folders and the owned workspace are exempt, so
nothing about families changes.

Left for follow-ups, each with its own design: design nodes and saved subharness runs
still run in the session workspace with the full belt; fork hands share their parent's
directory; the chat's own inline-edit allowance and a cross-session write lease; a sweep of
kept `task/*` branches.
