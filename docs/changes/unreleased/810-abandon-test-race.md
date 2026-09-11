---
kind: fixed
title: the abandon test reads its baseline before the next prompt goes in, so a fast turn is not a red
pr: 810
surface: [engine]
invalidates:
  - "`TestAnAbandonedTurnLeavesTheSessionFreeForTheNextPrompt` red in 0.00s with `never reached the model` was a race in the test, not in the session: it took the request count after `Submit` had already started the turn. It is now taken before, and a red there is a real defect."
---

The law is unchanged: the turn after an abandoned one answers, ends, and reaches
the model at least once. Only the reading moved.
