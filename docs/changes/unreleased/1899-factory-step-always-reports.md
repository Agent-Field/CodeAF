---
kind: fixed
title: A factory step that stops without reporting still ends with a report
pr: 1899
surface: [engine, chat, docs]
invalidates:
  - "Was true: a factory step that stopped without calling `stage_result` was reminded once (`stopped without reporting · asked once for the report`), a step whose turn ended in an error was not reminded at all, and a second silence ended the round as `the stage ended without reporting`. Is true now: a failed turn is tried once more on the same conversation (`the turn stopped on an error · trying it again: <why>`); then three reminders go (`stopped without reporting · reminded, 1 of 3` to `3 of 3`), the second repeating the step's ask and the third on a turn whose request requires `stage_result` through the provider's `tool_choice` (`3 of 3, on a turn that must report`); then the factory makes the report itself in one short call from the step's brief, its last message and its checkout's change, which must call `stage_result`. That report is marked `Inferred`, the log says `reported for the step · from its brief, its last message and its change`, its Timeline line starts `reported for the step ·`, and it is judged like any other. `the stage ended without reporting` is written only when all of that came back empty."
  - "Was true: a turn a spending limit refused ended the step as `the stage ended without reporting: daily limit reached · …` and the job asked whether to try again. Is true now: it is a stop: the step is not reminded, no report is made for it, and the job stops with `today's spend limit stopped this step` (another limit: `a spend limit stopped this step: <its words>`) and asks nothing."
  - "Was true: a stage conversation carried `quick_task`, and a person in the app could move a running stage's reply to a task. Is true now: a stage's belt has no `quick_task` (its reads were already never handed off), its page names only `propose_task` as the way to hand work out, and `MoveTurnToTask` refuses a stage with `a factory step's reply stays in its step`. The stage executor's folding of `read handed to quick task N` captions is gone with them."
---

On codeaf#1765 a plan step ran twenty-five commands, handed part of its
reading to a quick task, said it was finished, and was marked as a step that
never reported after one reminder. A model that forgot the closing call has
almost always done the work, so the round now asks harder, then requires the
call, then makes the report, and treats a spending limit as the stop it is.
