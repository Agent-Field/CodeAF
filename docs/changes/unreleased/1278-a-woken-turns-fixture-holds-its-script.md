---
kind: fixed
title: the woken-turn handover test no longer fails on a loaded machine
pr: 1278
surface: [engine]
invalidates:
  - "`TestAWokenTurnPastTheCeilingMovesToAFreshContext` failed about once in forty runs on a loaded machine and never on a quiet one. It was the test's fault and not the product's: the checkpoint fixtures pin the order of a reading and an answer through the turn's context, a submitted turn inherits that context from its caller, and a turn the agent wakes by itself has no caller and started under the plain background, so the fixture could reach every turn but that one."
  - "The agent has one unexported field, `wokenTurnBase`, that a fixture sets so a woken turn starts under the watched context. It is nil in the running product, which starts a woken turn under the background exactly as before. Closing a session ends a woken turn the way it ends any turn, through the turn's own cancel."
---

Nothing a person sees or a model reads changes.
