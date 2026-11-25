---
kind: internal
title: Fluid app records keep their IDs when sources change
pr: 2947
surface: [engine]
invalidates:
  - "Previously the data store registered its app grants again from current definitions on startup. A published app now commits its numbered snapshot, checked type bindings, and grants together."
  - "Previously record events carried before and after values in the outbox. The outbox now carries stable type IDs and replay reads those values under the receiving app's grants. Native subscriptions return an initial snapshot with its cursor and reset when their policy or query dependencies change."
---
