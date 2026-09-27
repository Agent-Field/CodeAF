---
kind: fixed
title: standing execution enforces git branch isolation and worktree validation
pr: 1560
surface: [engine]
invalidates:
  - "Standing task firings claiming branch isolation or granted permission only for branch commits could execute git commits directly onto the host workspace branch. The engine now verifies ref state and isolates git modifications to dedicated branches and worktrees."
---
