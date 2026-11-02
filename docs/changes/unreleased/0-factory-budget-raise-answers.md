---
kind: fixed
title: Budget recovery records the answer and requeues jobs without a runner
pr: 0
surface: [engine]
invalidates:
  - "Raising a held job's budget could resume its runner without recording answered:continue after budget_changed."
  - "A person answering continue to an unheld budget question could clear its open ask while leaving the job waiting."
  - "The timeline could say the job was carrying on before its continue answer succeeded."
  - "Manual jobs were left waiting on a budget question after a raise above spend answered it."
---

A raise above spend answers the open budget question in Auto and Manual. Manual
still asks before each next step. Take over, publication and other questions keep
their own decisions. A raise at or below spend keeps the budget question.

Budget recovery sends the chosen cap before answering Continue, records the
budget change followed by the answer, and confirms continuation only after
the answer succeeds. A job without a runner goes back to the queue with its
budget question taken down. Other open questions and Stop spending after this
step keep their own answers; a budget edit does not clear them.
