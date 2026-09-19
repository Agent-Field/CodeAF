---
kind: fixed
title: a run or a part you stopped reads stopped on its page, not incomplete
pr: 1258
surface: [chat, engine]
invalidates:
  - "A run a person stopped read `stopped` on the side list and `incomplete` on its own page and in the tasks place, at the same moment: the page reads the plan store, where a stopped task is `cancelled`, and `planStateWord` spelled `failed` and `cancelled` alike. On the real binary, hosted, the stopped run's page drew `incomplete` twelve times and `stopped` never; it now draws `stopped` thirteen times and `incomplete` never."
  - "`PlanTaskRow` carries `Stopped`, established by the session from the store and sent over the wire as a field. It is true when the task's own ending is a person's stop, and when the task was cancelled in the very instant an ancestor of it was stopped, which is what the store does to everything under a cancelled task. A part that failed, or was cancelled at another moment for the run's own reasons, keeps `incomplete`."
  - "`planStateWord`, `planStatus` and `planEnded` take the row, not the store's status string, so the word is decided in one place. `stopped` counts as ended: a stopped task is offered no verb and no next step, and it is not read as work that can still move."
---

Found by the hosted two-arm drive that closed #1253. The glyph follows the word
the way an ordinary stopped task's does.
