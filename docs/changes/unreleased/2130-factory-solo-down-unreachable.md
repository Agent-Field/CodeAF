---
kind: fixed
title: A solo factory that stops answering reads as unreachable, not signed out
pr: 2130
surface: [engine]
invalidates:
  - "With no credential held for a solo factory, factory.queue, factory.job and the other reads answered signed_out when the factory could not be reached, while factory.list said unreachable. These reads now answer unreachable, the same as the list. A factory that answers and asks for a sign-in still gives signed_out."
---

The desktop keeps the last rows it read for an unreachable factory and drops
them for a signed-out one, so a stopped solo factory blanked the Jobs list
instead of showing the kept rows.
