---
kind: fixed
title: a call the belt refused is no longer drawn as a step that ran
pr: 1260
surface: [chat, engine]
invalidates:
  - "A worker that answered one turn with several tool calls had the first one run and each of the others answered by the belt with its own refusal. Every one of them was recorded as a step, and a run's task page drew them as steps: a command a person reads as having run, with the belt's sentence to the worker in dim under it. On the real binary that was rows 2, 3 and 4 of a page, each reading `[not run] no action executed: return exactly one bash tool call per response`."
  - "A step now records whether its call ran (`Step.NotRun` in the run engine, set from the event's own `HarnessMade` fact, carried as `PlanStep.NotRun`). A call that did not run stays in the record and in the head's step count and has no row on the page. The surface never reads the refusal's sentence to find this out, and a record written before the field draws as it did."
  - "The same holds for every answer the harness writes itself instead of running a call: a withdrawn tool, a door that refused. None of them ran, and none is drawn as a step."
---

Seen on the hosted drive for #1257. What the belt tells the worker, and the rule
of one call per answer, are unchanged.
