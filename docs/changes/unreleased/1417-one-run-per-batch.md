---
kind: fixed
title: tasks approved together are one run, and a run that could not start says so
pr: 1417
surface: [chat, engine]
invalidates:
  - "Several hand-offs approved at the same moment each opened their own run. They raced to the conversation's one plan: two started runs over one path, one run's worker wrote its children and its `done`s into the other's plan, and the rest became tasks of the older engine. Now the first opens the run and every other joins it as a child."
  - "A run road that failed fell back to the older engine: a typed `/task` answered as if it had started, and an approved hand-off's receipt said `It runs on the older task engine, because the run engine could not start it:` (#1416). Neither falls back now. Both answer `task N did not start: <reason>`, the hand-off's receipt reads as a failure, and nothing starts in its place. Only a build with no run engine, or a conversation with nowhere to keep a plan, uses the older engine."
  - "A run worker was bound to its plan by path alone. It is now bound to its run's root too (`PLANDB_RUN`), and `plandb` refuses a plan at that path whose root is another run's."
  - "A message typed in a run row's room answered `no task N in this session`. It is now left as a note on the task's page; a row nothing drives says so and can be stopped."
---
