---
kind: changed
title: New factory jobs default to a five-dollar budget
pr: 1899
surface: [engine, chat, docs]
invalidates:
  - "Without a saved job budget or repository default, factory jobs previously started with a $3 ceiling. They now start with a $5 ceiling; existing budgets stay unchanged."
---

- New factory jobs without a saved budget or repository `default_budget` now use an up-to-$5 budget, previously $3.
- Existing job budgets and explicit repository defaults remain unchanged. The sidebar and Actions budget choices remain $5, $10, $20 and $30.
