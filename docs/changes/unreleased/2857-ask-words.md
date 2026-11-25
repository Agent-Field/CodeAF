---
kind: fixed
title: Questions after failed steps speak in your words, and continue knows a finished hand-off
pr: 2857
surface: [chat, engine]
invalidates:
  - "After failed steps the model was told to say plainly which steps failed, and it copied tool names and error text onto the question card (qloop run31: \"three `tasks continue` attempts … refused with no graph in this session\"). It is now told to say what changed, what did not, what is not done yet and what it needs, in the person's words, never naming a tool or a command or quoting an error."
  - "`tasks continue` on a run this conversation handed off was refused with \"There is no graph in this session for task 1\" even while `tasks` listed #1. It now answers from the run: an interrupted run carries on, a done one says \"is done, so there is nothing to continue\", a running one is told to take a line with say. The refusal for a task this conversation truly does not hold now reads \"Task N is not one this conversation holds\" and no longer says graph."
---
