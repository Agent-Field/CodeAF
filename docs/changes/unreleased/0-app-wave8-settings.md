---
kind: added
title: codeaf app serves every row of the Settings window
pr: 0
surface: [engine]
---

Was true: the app's settings covered the model, money, clock, idle days and
work folder only.

Is true now: settings carry appearance, text size, open at login, the dock
badge, pinned models, the Checks and Planner models (the crew's high and
planner rows) and the fallback model; `rules.list`/`rules.remove` read and edit
the always-allow rules, `memory.list`/`memory.forget` the memory store,
`spend.export` writes the usage ledger as CSV, and `models.pin` keeps three
pinned models.
