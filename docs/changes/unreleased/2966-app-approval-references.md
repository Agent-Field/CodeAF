---
kind: fixed
title: Point app builders to valid approval and loop inputs
pr: 2966
surface: [app, engine]
invalidates:
  - Check used to wait for an example run to expose a field read from plain agent text. Check now points to the invalid approval argument or loop input during definition validation.
---

The builder can load a typed example that saves drafted replies and carries
their exact text into a send approval. If a run reads a missing step or an
unavailable field from plain agent output, Check names the binding to fix.
