---
kind: added
title: A job's steps can be removed and moved for that job, by its person or its manager
pr: 2013
surface: [engine, chat]
invalidates:
  - "Was true: one job's steps could only be added, reworded, given thinking, switched on or skipped, so a manager asked to remove a step could only skip it. Is true now: factory.Edit also removes a step from the job and moves one after another or first, recorded as removed <name> and moved <name> after <stage>."
  - "Was true: factory.job_step_edit refused remove and move as unknown fields. Is true now: it carries remove and move (a move names its after, \"\" for first) and hello announces factory.job_step_edit_structure."
---

Remove and move hold every bound the other parts do: a step that has begun,
proof, a fixed step and a step a policy names stay where they are, nothing is
moved before a begun step or after proof (but a person's approval or a post),
only the person removes or moves an approval or a post, a post is never moved
before the proof or approval it stood after, the adapt word holds, and an edit
is whole or nothing. `factory_run` and the `factory_item` card offer both to the job's
manager; the plan stage's `plan_edit` does not.
