---
kind: fixed
title: Running a factory job goes straight to its first step
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: before the first step of a run, the runner gave the job's manager conversation one turn of its own, asked `Shape the run for this item now.` with what the person had typed, and applied whatever it set; a turn that read files could stop at the chat's quick-turn limit and a task card nobody answered, and the plan never started. Is true now: a launch gives the manager no turn. The run starts at its first step with the steps the job has, which are the repository's pipeline unless the person, or the manager on the person's words through `factory_run`, changed them first."
  - "Was true: a launch logged `the recipe stands`, `the manager did not answer · the recipe stands`, `the manager was busy in the window · the recipe stands` or `the manager is open in another window · the recipe stands` before its first step. Is true now: a launch says none of them; they come only from `shape steps`, which a run never asks for."
  - "Was true: `internal/factory/run.Options` had `Shape`, the launch's turn, and `ShapeNow` fell back to it. Is true now: `Shape` is gone and `ShapeNow` is the only shaping turn; a worker without it answers `shape steps` with `the manager did not answer · the recipe stands`."
---

The owner pressed Run on an issue job and the plan never started: the manager's
turn before it read files, hit the chat's quick-turn limit and waited on a card.
That turn is removed rather than repaired. The steps change where a person
changes them, on the job or in words to its manager; the manager's
`factory_run` during the person's own turn, the inbox and `shape steps` are
unchanged.
