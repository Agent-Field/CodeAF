---
kind: added
title: A job's person may add a check step to that one job; only they change a check's command
pr: 0
surface: [engine, docs]
invalidates:
  - "Was true: a step added to one job (the step-edit route, the manager's `factory_run`, the plan) was a conversation or an approval, never a check; and any of them could reword any step's ask, a check's command included. Is true now: the job's person may add a check to their job (kind check: the ask is the shell command run in the job's checkout, until done, clean, green or proven with green when it says none, and max 1 to 9 rounds; no effort or fanout). Only the person adds a check or changes a check's command: the manager and the plan are refused, since a check runs whatever it says on the runner's checkout. A check goes before proof unless placed, like any added step."
---
