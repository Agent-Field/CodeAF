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
before the proof or approval it stood after, nor that approval after the post,
the adapt word holds, and an edit is whole or nothing. `factory_run` and the
`factory_item` card offer both to the job's manager; the plan stage's
`plan_edit` does not.

Every door lays the run's phases not yet begun again from the edited steps,
queued or running, so a removed step never runs: the person's and manager's
edits, the local seam's edit (through the lease's `EditSteps` when its runner
has one) and the `factory_item` card (through the lease's new `ChangeSteps`,
which also withdraws a worker running the job from its own copy). A factory
older than remove and move refuses them as unknown fields, and that is said as
`This factory doesn't support that yet; nothing was changed. Update the
factory to use it.`
