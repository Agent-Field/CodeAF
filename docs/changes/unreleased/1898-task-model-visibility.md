---
kind: fixed
title: task pages keep saved crew models visible without a priced call
pr: 1898
surface: [chat, engine, remote, docs]
invalidates:
  - "A run task page needed a saved spend row to name a model. It now also reads the run's saved worker, planner and checker assignments, including for archived children after an interruption; those assignments are separate from historical usage."
  - "Task links and arrow navigation could open an ordinary task room for a run and offer a model picker that could not apply. Known run tasks now open the same stored page as sidebar clicks, with explicit editing limits."
  - "A program's declared model pool was visible only in the footer. Task setup now lists its models and thinking level; missing history is shown as not recorded instead of silently omitting the model row."
---

This changes visibility and navigation. Run and program models remain fixed for
an existing run; ordinary task model controls retain their existing scope.
