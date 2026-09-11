---
kind: fixed
title: A task no longer pays for a fork it will not use, and its runner reads git once a batch
pr: 881
surface: [engine, docs]
invalidates:
  - "The ground ladder's top rung (a furrow fork of the whole folder) fell through to the snapshot rung in silence, and it was tried again on every task: on the owner's laptop 93 of 93 nodes ended up on the snapshot rung after paying about 15 s each for a fork that was thrown away. Now the node's log says why the fork fell and what it cost (`a fork of the whole folder was tried and could not be made · <time> · <reason>`). The fall is remembered per folder in ~/.aforge/v3/universe-falls.json, so later tasks there skip the fork before touching furrow and log `a fork of the whole folder was not tried`. The fork is tried again once the aforge build or its furrow changes."
  - "The fork rung sealed the parent's world with `git checkout -b` and `git commit` inside the fork, which ran the repository's commit hooks and signing config, so a commit-msg or pre-commit hook fell the rung and the task lost its `.env`. sealForkWorld is deleted: the fork is sealed by sealGroundWork, the same commit-tree door the snapshot rung uses, and its branch is cut by standOnSeal, which runs no hooks."
  - "Workspace.Fork had no timeout. It now shares the attach's one-minute bound (wholeWorkspaceTimeout, formerly attachTimeout), and a merge preview uses the read bound. furrow.Attach returns (*Workspace, error) with furrow's own reason, and ErrNotHere when there is no furrow. TestEveryCallIntoFurrowIsBounded fails any unbounded call into furrow that is not declared person-paced."
  - "The task runner ran `git status --untracked-files=all` synchronously for every finished tool call (worktreeMoved, now deleted). It now reads the tree at most once per batch, only when the batch holds a call that could have changed it (not a reading hand, a harness refusal or a named save); a batch that only saved spends that one reading at the end of itself, so the fingerprint never goes stale. The read runs beside the drain as an offpath.Reading and uses --no-optional-locks, so it no longer takes the index lock the worker's own git needs."
  - "The manual said aforge never runs `furrow watch` on a folder by itself. That has been false since the ladder began attaching the folder a task is about to fork, and the page now says so."
---

A task's first request no longer waits on a fork that has already failed on that folder,
and the log says where the seconds before the first request went. The worker's brief is
unchanged. The trade is stated in groundfalls.go and docs/design/task-start/DESIGN.md: a
fall that was a coincidence stands the fork down for that folder until the next build.
