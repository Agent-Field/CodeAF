---
kind: fixed
title: standing execution enforces git branch isolation and worktree validation
pr: 1560
surface: [engine]
invalidates:
  - "Standing task firings claiming branch isolation or granted permission only for branch commits could execute git commits directly onto the host workspace branch. The approved does.isolate setting now selects a dedicated Git branch and worktree. Permission and reply prose no longer decide isolation. Uncommitted work and its recovery record are retained."
---
