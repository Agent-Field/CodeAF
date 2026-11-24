---
kind: changed
title: A Factory job's budget can be any amount from one dollar up
pr: 2789
surface: [engine, factory, app]
invalidates:
  - "A Factory job's budget could only be $5, $10, $20, $30 or exactly the job's next cap; `factory.job_set_budget 1.5` and a create `budget` of 7 were refused. Now any amount from $1 up, to the cent, is accepted on factory.job_set_budget, factory.job_create, POST /v1/jobs/{id}/raise_budget and the create budget on /v1/jobs and /v1/issues; less is refused with \"A job's budget is at least $1\" and nothing changes."
  - "The $5, $10, $20 and $30 budget steps were the only allowed amounts. They are now suggestions: the presets a picker offers and the ladder a cap question's next budget climbs."
---

Owner decision 2026-11-17 §12. `factory.MinJobBudget` is the one floor and
`factory.CheckJobBudget` the one check every door uses; an accepted amount is
rounded to whole cents. A budget is still never set below recorded spending.
