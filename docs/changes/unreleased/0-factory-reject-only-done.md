---
pr: 0
kind: fixed
title: A refused attempt withdraws only an earlier success
surface: [engine]
invalidates:
  - "Was true: refusing a newer completion report turned the earlier report into a checkpoint whatever it said, hiding an incomplete report's blocker. Is true: only an earlier done report is withdrawn."
---

When a step's newer attempt is refused, a stale success must not stand in for
it, so the earlier done report is withdrawn. An earlier report that was not a
success (a blocker, a request for a person) is kept as it was.
