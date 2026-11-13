---
kind: fixed
title: Model pool errands stop with the process or window that started them
pr: 2595
surface: [chat, engine, app]
invalidates:
  - "One tracker belonged to each profile, so the first process or window to close cancelled every owner's pool errands and left surviving owners' later work untracked. Each owner now retains its own tracker, cancellation context and bounded join; closing one owner preserves the others' work and tracking."
---

Closing an owner cancels and joins its index refresh, outbox push, model warms
and judge work within the existing one-second pool shutdown budget. Repeated
closes cannot release another owner's hold, and the last close leaves no tracker
for the profile. Window startup refreshes and pushes still start per wiring, while
headless invocations keep the shared catalog's one-time startup work.
