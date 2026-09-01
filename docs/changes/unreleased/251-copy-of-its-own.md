---
kind: fixed
title: parts work in a copy of their own, not a copy of the repository
pr: 251
surface: [engine, chat]
invalidates:
  - "divideDescription, divideReviewBrief, divisionDone, the task start notice and prompts/system.md still said each part works in a copy of the repository. They say a copy of its own / a working copy now, which is true of a folder family as well as a repository one."
  - "divisionTooNarrow and divisionNoLane told the worker a refused split would have cost a copy of the repository apiece. They say a working copy now."
  - "internal/manual/chat/tasks.md said a task works in a worktree of your repository. The card law forbids that word on a person-facing page; it is a working copy of your repository now, and the three cost sentences on the same page say working copy rather than copy of the repository."
---

#239 cut folder-family parts off a mirror of their own. The mechanism was
right; four tool strings and the tasks page still promised the old shape.
