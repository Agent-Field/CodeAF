---
kind: fixed
title: Token usage reaches telemetry while a session is still running
pr: 1731
surface: [docs, engine]
invalidates:
  - "Token telemetry was reported only when a session ended. It is now queued as additive usage deltas and flushed every 30 seconds while the session remains open."
  - "`codeaf telemetry on` changed the profile but could leave the notice send gate closed. Explicit opt-in now opens that gate immediately."
---
