---
kind: fixed
title: A raised budget resumes an Auto job its runner parked at the cap
pr: 2169
surface: [engine]
invalidates:
  - "Was true: raise_budget resumed an Auto job only when its desk copy had no open ask. A worker's report puts the cap question on that copy as Asking, so a held job parked at its cap stayed parked. Is true now: when the open ask is the cap question and the new cap clears the spend, the raise answers it \"continue\" through the answer door a person's answer uses. The ask comes down, the ledger records budget_changed and then answered, and the timeline says the budget was raised and the job carries on."
  - "Was true: the budget worker signal's yes asked the worker to answer continue itself. Is true now: the budget signal carries only the cap. The continue arrives as an answer signal after it."
  - "Was true: a continue to a cap question on a job no worker holds, with the ask still on it, was kept for a later round and left the job needing you. Is true now: the job is queued again with its question taken down."
---

Manual jobs, any other open ask, and a raise to the spend or lower still
leave the job parked. Only the cap moves.
