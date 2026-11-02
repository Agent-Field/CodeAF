---
kind: removed
title: The factory's shape steps is gone
pr: 1899
surface: [engine, chat, docs]
invalidates:
  - "Was true: `POST /v1/jobs/{id}/shape` (shape steps) gave a job's manager one turn to reshape its steps, on the factory or through a `shape` lease signal to the worker holding the job, and said `the recipe stands`, `the manager already shaped the steps · say what to change in its chat` and the busy, away and refused lines. Is true now: the route, the `Seam.Shape` door, `lease.SignalShape`, `run.Options.ShapeNow`, `Runner.ShapeAsked` and every one of those lines are gone; nothing in any client called it. The steps change where a person changes them: on the job, or in words to its manager, whose own `factory_run` applies at once."
  - "Was true: the manager's turns in cmd/codeaf (`shapeTurns`) served shape steps and the inbox. Is true now: they serve the inbox alone; `ShapeWait` bounds an inbox turn."
---
