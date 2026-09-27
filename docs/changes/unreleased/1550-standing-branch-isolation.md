---
kind: fixed
title: standing execution enforces git branch isolation and worktree validation
issue: 1550
surface: [session, engine]
invalidates:
  - "Standing task firings claiming branch isolation or granted permission only for branch commits could execute git commits directly onto the host workspace branch (e.g. main). The engine now verifies ref state and isolates git modifications to dedicated branches/worktrees."
---

Enforce worktree and branch isolation for standing task firings claiming branch-isolated commits (#1550).
