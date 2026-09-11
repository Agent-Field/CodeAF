---
kind: internal
title: Observe streamed checks before cutting the second-reading regression fixture
pr: 653
surface: [build]
invalidates:
  - "The second-reading retention test assumed its shell emitted six checks within 900 milliseconds of launch. It now observes an emission marker before cancellation and requires all six identities to survive."
---

The fixture follows the first-reading test's synchronization: a ten-second
readiness bound observes output, then cancellation cuts the still-running suite.
A thirty-second fixture reading budget and bounded five-second cleanup join keep
failures finite; they do not change any production reading budget. The existing
timeout, partial-roster and no-subtraction assertions remain in place.
