---
kind: added
title: The factory says which models its jobs used
pr: 0
surface: [engine, docs]
invalidates:
  - "Was true: a factory job's model calls were rows on the machine's spend ledger with nothing naming the job, so no read could say which models the factory's jobs used. Is true now: every call made for a factory job (its steps, its own chat and their children) carries `factory_job` (`owner/name#number`) on its ledger row, and the app's `factory.usage_by_model {factory, from, to, job?}` (hello word `factory.usage_by_model`) answers, for this machine's factory, each model's calls, input and output tokens, how it is paid (`metered`, `plan`, `local`, `free`; absent when unknown) and its priced dollars (absent when no call was priced, with `unpriced_calls`), over every job and per job. Calls another computer made for a job are on that computer's ledger and are not counted here."
---

Spending can show a factory's usage by model the way it shows the person's
own: model, how it is paid, tokens and dollars, never a dollar figure for a
call nobody priced.
