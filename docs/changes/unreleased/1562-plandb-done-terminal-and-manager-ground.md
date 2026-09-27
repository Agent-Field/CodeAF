---
kind: fixed
title: plandb done refuses terminal tasks and manager tasks derive project workspace
pr: 1562
surface: [engine, resident]
invalidates:
  - "plandb done on a cancelled task failed with 'task is not claimed'; it now explicitly reports that the task is already terminal."
  - "a task proposed by a manager whose workspace was ~ inherited ~ as folder ground; it now falls back to Place.Workspace."
---

When a task was cancelled upstream, plandb done called requireOwner first, which failed with a misleading 'task is not claimed' error because cancelled tasks clear their claim. Now terminal status is checked before owner verification (preserving placeholder auto-completion).

In addition, groundLadder now falls back from a non-repository workspace (such as ~) to a configured project Place.Workspace rather than landing tasks directly in ~.
