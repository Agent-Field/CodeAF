---
kind: added
title: settled tasks grade the models, and the divider reads that record before it assigns a tier
pr: 155
surface: [chat, engine]
invalidates:
  - "`aforge models` printed `nothing measured yet. Ratings appear once calls have been graded.` on a machine that had run tasks for weeks, because only internal/router ever wrote to the ledger and the v3 chat engine does not route through it. Every settled task node now writes into that same ledger, under the class `task.node/<the work's own name>`."
  - "A part's tier was decided by one word the dividing worker wrote — `mechanical` keeps the task's model, `careful` resolves through the high tier — and nothing else was consulted. The store is read first now: a part called ordinary work is minted on the careful tier when work of the same kind-shaped name has been turned down by the check twice or more on the model the task is on. A part the worker graded careful is never demoted, and an install with no crew classes set still lifts nothing, because the ladder floors on the task's own model."
  - "`internal/router`'s ledger held only routed model calls, so a class was always one of the harness's own passes. `provider.ClassTaskNode` is a WHOLE settled piece of work, and `router.Event` carries two more fields for it: `outcome` (the settle's own words — `landed`, `not accepted`, `needs your look`, `stopped`) and `retries` (the repair rounds the check spent)."
  - "`router.shaped` was unexported and the router was the only thing that made a ledger key. It is `router.Shaped`, because two writers now make keys and two spellings would be two populations."
  - "`aforge models` laid its class column out at a constant 22 characters. It is measured from the rows and capped at 40, so a long kind does not step every row after it sideways."
---

Nothing extra is spent to learn any of this. The check at the end of a task had
already read the work and said whether it holds, so that answer IS the grade — it
is written on the node where it is given and read off the node when the node
settles. Work nobody could check writes its record and moves no rating, which is
the ledger's own law about outcomes that are not evidence. What changes is that
"this kind of work is mechanical" stops being a guess a prompt taught and becomes
a fact somebody measured, per install.
