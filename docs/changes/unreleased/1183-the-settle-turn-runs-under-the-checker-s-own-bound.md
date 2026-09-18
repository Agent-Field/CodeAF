---
kind: changed
title: a settle turn runs under the checker's own bound, not the run's wall
pr: 1183
surface: [chat]
invalidates:
  - "The turn a landing note wakes under `task.settle = auto` — the one that reads the work and settles it — was an ordinary full-belt turn with no bound of its own: no call ceiling, no dollar bound, no window. In one measured run it was a single 28-minute turn of 49 tool-call rounds on the high-tier model, stopped only by the run's wall, over a tree that was already clean. The wake is now marked a settle wake and the turn it starts runs under the checker's own contract: a call window it is told (the model reads the clock the same way the checker does), a ceiling of `settleCallCeiling` provider calls, and — when the run carries a ceiling of its own — a share of the run's money. When one of those trips, the node comes back to the person with the reason and the count on its report (`it was not settled within its bound — N calls`) instead of stopping silently. A node whose own check saw a clean tree with no declared check settles after a single call, and a conversation's ordinary turn — and every wake that is not a landing handing over a decision — keeps no ceiling, exactly as before."
---
