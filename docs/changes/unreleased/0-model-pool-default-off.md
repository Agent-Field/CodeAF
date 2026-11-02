---
kind: changed
title: Model Pool stays off until you choose it
pr: 0
surface: [engine, chat, app]
invalidates:
  - "The Model Pool previously enabled itself on fresh profiles and used read mode in CI."
---

With no saved choice, the Model Pool is off, including in CI. Your configured
model and provider answer without pool judge calls, routing or records.
Explicit saved on, read and off choices remain honoured; CODEAF_MODEL_POOL
still overrides the saved choice. Settings and the manual now say off by default.
