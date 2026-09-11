---
kind: fixed
title: a waiting question rides behind the row's door instead of over it
pr: 840
surface: [chat, docs]
invalidates:
  - "On the roster, a note a place had to say (`placeMsgLine`) replaced the whole foot, so a task whose run raised a question while its row was selected drew `hello.txt · waiting in this conversation · alt+a` on the one line `enter open its room` lived on — and the row lost its door. The note now rides BEHIND the door on the same line (`hintFitBeside`), and the page's own clauses — fold, verbs, filter, the way out — are the ones that give way when the width runs short, never the door and never the note. `questionWaitingWord` still reads on that foot, now beside `tasksEnterRoomWord` instead of instead of it."
  - "A place's one refusal line was flat-dimmed and one field; it is now painted by the hint's own key grammar ([paintHint]) on the hint's own line, so its keys keep the colour every other key on this surface wears."
---

A task's question is a fact about the row's work, and the door is the row's
own key — the choice between them that `#840` measured was never a choice.
The e2e suite now stages the failure the issue caught only by luck: a scripted
wire ([questionfoot_e2e_test.go] in internal/e2e) answers the task's own request
with an `ask` call every run, so the door hint and the waiting note are both
read on the one line the foot draws them on — `enter open its room · … ·
waiting in this conversation · alt+a` — instead of waiting on a line a question
can displace.

New tests: `TestAWaitingQuestionRidesBehindTheRowDoorOnOneLine` (the pair, and
the way out gives way before the door does), `TestANotelessFootKeepsTheDoorHintAsToday`
(the control), `TestTaskQuestionFootKeepsTheRowDoor` (the staged e2e).
