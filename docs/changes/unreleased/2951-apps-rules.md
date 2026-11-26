---
kind: added
title: Fluid apps assign work, route approvals and protect booking slots
pr: 2951
surface: [engine]
invalidates:
  - "Was true: assignment, approval routing and booking constraints were declarations. Is true now: the record writer creates assigned work on create and transition, selects approval steps, and refuses overlapping or over-capacity bookings with the conflicting record."
  - "Was true: a current signature satisfied signed() by itself. Is true now: when approval routing applies, signed() requires every current step. A maker-checker rule requires a second person, so it stays blocked in Personal."
---
