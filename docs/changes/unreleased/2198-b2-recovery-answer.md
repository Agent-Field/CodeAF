---
kind: internal
title: the B2 recovery test requires its worker's answer, not only a settled state
pr: 2198
invalidates:
  - >-
    Was true: the B2 startup recovery test passed for any settled state, so a
    task that failed without its worker running also passed. Is true now: it
    rejects Failed and requires the worker's answer on the report.
---
