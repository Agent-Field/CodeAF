---
kind: fixed
title: Budget questions and raise buttons agree on the next cap
pr: 0
surface: [engine]
invalidates:
  - "Yes doubled a job's old cap while the desktop raised it to an allowed budget."
---

A budget question now names the amount spent and the next total budget. Yes,
continue and the desktop's raise button at that amount all use the smallest
of $5, $10, $20 or $30 above recorded spend. Older saved questions use the same
rule when answered. At $30 spent or more, the next budget doubles the current
cap repeatedly until it exceeds spending. The raise button accepts that exact
next budget as well as the standard choices.
