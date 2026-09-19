---
kind: fixed
title: a call that did not run is no longer drawn as a step that ran
pr: 1260
surface: [chat, engine]
invalidates:
  - "A worker that answered one turn with several tool calls had none of them run, and each was answered by the belt with its own correction. Every one of them was recorded as a step, and a run's task page drew them as steps: a command a person reads as having run, with the belt's sentence to the worker in dim under it. On the real binary that was rows 2, 3 and 4 of a page, each reading `[not run] no action executed: return exactly one bash tool call per response`."
  - "A step now records whether its call ran, and if not, which of two things it was. `Step.NotRun` in the run engine is set from the event's `HarnessMade` fact. `Step.Refused` is set from a new event fact, `Event.Refused`: an action the worker attempted and a door refused, set at the one place every pre-action veto passes through. Both are carried on `PlanStep`. The surface reads the two fields and never the answer's words, and a record written before the fields draws as it did."
  - "A correction about the FORM of a reply (several calls in one answer, a call that could not be read, a tool that is not on the belt, a held process rule) has no row. It stays in the record and in the head's step count."
  - "AN ACTION THE WORKER TRIED AND WAS REFUSED IS DRAWN, as one dim line in the step's place: `refused`, the word the permissions page already uses, and the command as typed. It has no number, because a number on that page is a step that ran, and the rows around it keep their recorded numbers. The answer written for the worker is not drawn."
---

Seen on the hosted drive for #1257. What the belt tells the worker, the rule of one
call per answer, and every refusal's wording are unchanged. The manual said a batch
had its first call run. It does not: the whole batch is answered and none of it runs.
