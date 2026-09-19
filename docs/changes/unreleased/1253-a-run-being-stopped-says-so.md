---
kind: fixed
title: a run you stopped reads stopping, not running, until it has ended
pr: 1253
surface: [chat, engine]
invalidates:
  - "Between `stop it` and a run's ending its row went on reading `running`. The engine answers a stop once every worker is home, and a worker inside a step can take seconds to end: 7.09 seconds in one hosted drive on 2026-09-19. The row is now published as still running and stopped by a person the moment the stop is taken, which is the reading a stopped task's row already has, and it settles as `stopped` when the run has ended."
  - "A second `stop it` in that window answers that the run is already stopping. It did before as well; what is new is that the screen agrees with it."
---

Found by the usage-ledger probes for #1250. Nothing about what a stop does
changes here: the store is written first, the context is cut next, the work is
kept on the run's branch last. The ledger law over publishers of a running row
lists the new publisher with the test that proves it.
