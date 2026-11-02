---
kind: fixed
title: A viewer limited to some repositories sees their spend today, not on every day
pr: 2146
surface: [engine]
invalidates:
  - "Was true: a viewer who could read only some repositories saw capacity.daily summed over every day the visible jobs ran. Is true now: it counts only jobs last changed today, as the owner's floor does."
---
