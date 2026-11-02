---
kind: fixed
title: Closing the factory waits for its setup worker, so nothing writes after it has closed
pr: 2153
surface: [engine]
invalidates:
  - "Was true: closing the factory cancelled a running setup worker and returned while it could still write into the factory folder. Is true now: Close waits until that worker has returned."
---
