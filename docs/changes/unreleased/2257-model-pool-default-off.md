---
kind: changed
title: Model Pool stays off until you choose it
pr: 2257
surface: [engine, chat, app]
invalidates:
  - "The Model Pool previously enabled itself on fresh profiles and used read mode in CI."
---

With no saved choice, the Model Pool is off, including in CI. It makes no
pool judge calls, routing or records. Each role still follows your task-model
and tier settings, and in Auto the router picks from your allowed models.
Explicit saved on, read and off choices remain honoured; CODEAF_MODEL_POOL
still overrides the saved choice. Settings and the manual now say off by default.
