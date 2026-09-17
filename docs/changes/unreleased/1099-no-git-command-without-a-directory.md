---
kind: fixed
title: No git command runs without a directory, and the checkout guard reads a linked worktree's own share
pr: 1099
surface: [engine]
invalidates:
  - "`worktreeDirtIn` and `UnsavedEditsNote` built `git -C <dir>` themselves, and `git -C \"\"` is a no-op, so an empty directory ran the command where the process stood. Both go through `gitWith` now (`gitContext` is the same call under a caller's context), where the refusal of an empty directory and the pinned environment live once."
  - "`taskTree.releaseLanded` removed a worktree and deleted its branch with no empty-root guard and without the root lock. It refuses a tree with no root or directory and takes the root lock, as `releaseKept` does."
  - "The session package's checkout guard read branches and worktree registrations for the whole shared clone, so from a linked worktree a sibling's task/* churn was reported as the run's damage, and its live-codeaf probe walked `/proc`, which does not exist on darwin. From a linked worktree the branch reading is skipped and the worktree reading keeps only registrations under the checkout; the probe reads the process table."
---

A command with no directory runs wherever the process happens to be. Every
spelling of a git call in this package is a wrapper of one function that
refuses that, and the two that had gone round it are wrappers of it now.
