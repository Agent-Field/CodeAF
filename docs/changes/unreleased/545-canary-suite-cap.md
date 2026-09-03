---
kind: fixed
title: a canary cell whose whole suite is cut at the cap still gets its grade
pr: 545
surface: [build]
invalidates:
  - "A canary row reading `no grade` with its fix tests green meant the judge could not grade the cell. When the project's whole suite hit the 600 s cap, the judge died on the cut output (bytes concatenated with a string) before writing its record, so any slow suite — tox, both cells of the 774a543c/79515001 pair on #407 — read `no grade`. The cut output is decoded, the suite is recorded as capped as before, and the fix-test verdict stands."
---
