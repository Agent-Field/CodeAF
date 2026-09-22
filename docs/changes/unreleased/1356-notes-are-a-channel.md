---
kind: changed
title: a note on a task reaches that task's running worker, and the chat can read the notes back
pr: 1356
surface: [engine, chat, docs]
invalidates:
  - "A note left on a plan task was read by the screen and by nobody else. The worker saw it only if it happened to run `plandb task notes`, which nothing gave it a reason to do, and the conversation could list every row of a run and never learn that a worker had written down that another task's premise was wrong. A note addressed to a task is now handed to that task's worker between its steps, and the chat's `tasks` listing carries each row's newest note while a task's page carries them in full."
  - "internal/manual/chat/worker-harness.md said a note was one 'which the worker reads in its next frame', and session.PlanNote and tui3's taskPlanNoteSend said the same in their doc comments. None of it was true when it was written: nothing on the bash road read notes at all. It is true now, by a different mechanism and in different words — the worker is handed the note between its own steps — and the three places that claimed it have been corrected rather than deleted."
  - "The `tasks` tool's schema was one string every conversation carried. It is now two: the plan road's own `note` field is offered only where a plan store exists to hold it (Config.oneTaskRoad), so a conversation whose hand-offs are nodes of the session tree carries exactly the bytes it carried before."
---

The delivery bound is `notesPerDelivery` in `internal/run/bashworker.go`: five
notes at one step boundary, and what the bound leaves behind is still unread, so
the next boundary carries it. The mark that stops a second delivery is the
worker's own and lives only for the life of its loop — the screen consumes
nothing, which is what keeps a note the person opened from being a note the
worker never sees.

A note cannot move what a task is judged by. The sentence the worker reads says
so, and `TestANoteDoesNotChangeWhatATaskWasAskedFor` asserts the task's own work
order against the store after a note that reads like an order.
