---
kind: fixed
title: A task's ground never climbs out of the scratch directory it lives in
pr: 585
surface: [engine]
invalidates:
  - "A task's ground was whatever repository `git rev-parse --show-toplevel` found above its workspace — no longer true. A workspace inside the machine's scratch (`GOTMPDIR`, `TMPDIR`, `/tmp`, `/private/tmp`, `/var/folders`) never reaches a repository sitting above that scratch directory; it stands on a repository inside the scratch, or on none at all and works in place."
  - "`git(\"\")` was the way a run reached a repository the harness did not own (#402, closed) — it was not the only way. A git command given a directory it does own, which then resolves to a repository above that directory, is the second road, and it is the one that moved a person's HEAD."
  - "The hermetic checkout guard watched the head and the porcelain — it now also watches `git branch --list` and `git worktree list`. A new `task/*` branch with no head move, and a worktree registered under gitignored `.aforge-v3/`, were both invisible to it and both happened."
  - "`scratchPath` was the only reader of the scratch list. There are two now, and they read it in opposite directions: one exempts a write, the other refuses a ground, so `GOTMPDIR` is on the refusing list alone."
---

A `go test ./internal/session/` run wrote a real commit onto the branch of the
checkout it was launched from — `task: Paint`, four `marketing/sheet-*.png` files,
authored by `aforge` — along with eighteen `task/*` branches and a live registered
worktree. The tests themselves passed; only the guard failed, after the damage,
and the second road left no trace at all.

The whole precondition was a temporary directory that happened to sit inside a
checkout, which is what `GOTMPDIR=<worktree>/.gotmp go test ./...` gives you. A
node's workspace is then a `t.TempDir()` several levels inside somebody's project,
`git rev-parse --show-toplevel` walks up and out of it and answers with the project,
and the ladder reads that as a person standing in their own repository: cut a branch
off their HEAD, commit the node's work, merge it home. The environment decided
whether the suite committed into somebody's tree; the code was the same either way.

So the reading stops at a boundary it did not have. The machine's scratch is where
work is put down, never what work is about, and a repository that merely contains the
temporary directory is a repository the task was never given. It is checked one scratch
root at a time rather than in aggregate — `/tmp` is itself a scratch root, so a checkout
under it is inside one, while the `.gotmp` beneath that checkout is the boundary actually
crossed. A repository made inside the scratch still answers, which is what every test
that builds its own repository in a `t.TempDir()` relies on.
