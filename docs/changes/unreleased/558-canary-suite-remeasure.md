---
kind: fixed
title: the canary whole-suite column runs past collection errors, and an anchor can be remeasured
pr: 558
surface: [build]
invalidates:
  - "A canary suite column on tox or reef compared the door's tree against a base that collected nothing (`passed 0`, one or six import errors), so it said nothing and could flag a regression the run never caused. The whole suite now runs with `--continue-on-collection-errors` in both the judge and the base measurement; a base that still collected nothing renders `base ⊘`; reef's base is 1370 passed, 10 failed, 6 errors; tox's whole suite exceeds the 600 s cap in a clean clone and is recorded as not measured."
  - "A pool entry whose install rung stopped resolving (virtualenv, `--group dev`, pip resolution-too-deep on 2026-09-03) could only be fixed by hand. `pick.py --remeasure REPO` re-walks the ladder from the recorded rung and rewrites `install`, `base_suite` and a `measured` date in place."
---
