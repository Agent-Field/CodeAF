---
kind: fixed
title: Recover approved local app writes after a restart
pr: 2969
surface: [app, engine]
invalidates:
  - An interruption after preparing a local record write used to leave the run incomplete. A run with a receipt key now recovers the write and continues.
---

The record changes, their trigger events, and the step receipt commit together.
On restart the run can make a pending write or recover the committed result
without repeating it. Older prepared writes without a receipt key stay incomplete.
