---
kind: internal
title: Verify retries after command steps run out of attempts
pr: 2232
surface: [engine]
invalidates: []
---

Regression coverage verifies that Try again runs an exhausted command twice
without waiting for a runner timeout, and that a successful retry advances to
the next step. It uses Auto with the default worker slots and checks that each
command actually executes. The existing replacement-lease cleanup fix covers
these cases; no additional behavior change is needed.
