---
kind: fixed
title: step edits keep a slow-to-stop job running and need a named step; the foreman is the Factory manager
pr: 2490
surface: [factory, engine, chat]
invalidates:
  - "Was true: after a step edit withdrew the worker, the next lease's go-on was dropped when the withdrawn round was still letting go of a model call (the runner read the cancelled, unpaused control as unchanged); the old drive then ended, nothing ran the job, and it fell back to new in Backlog with Write still marked running. Is true now: go-on on a cancelled control whose job the store holds paused waits for the old drive and revives the job at its step."
  - "Was true: a step the manager or plan added from a sentence with no `name:` (factory_item add, plan_edit) was silently named by the sentence's first word and, with no place at its front, put just before proof; factory-demo#168 got a step called Immediately. Is true now: a manager's or plan's nameless sentence whose first word is not a shipped step or one of the job's recipe is refused, the whole edit with it, in words telling the model to name it (`vet: run go vet ./...`); a person's typed sentence still names by its first word, and factory_run always takes a name."
  - "Was true: the factory-wide manager's conversation and team word were titled `foreman`. Is true now: they read Factory manager; the team handle stays `@foreman`, the conversation is still found by the store's mark, and a foreman made before is retitled when it next opens, not made again."
  - "Was true: factory_floor printed a job following its pull request as `shipped` beside an unfinished step count with no step named. Is true now: it reads `following its pull request (Follow PR)`, `on Follow PR`; a shipped job with nothing left to follow reads `done`."
  - "Was true: TestFactoryManagersE2E said nothing about a job that stalled after proof. Is true now: it logs each change in what the landed job waits on (state, reason, approval, publication, claims not shown, Auto's block reason) and keeps job-<id>.json beside the logs."
---
