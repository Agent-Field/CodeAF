---
kind: fixed
title: The app opens factory stages running in its own process
pr: 1855
surface: [engine]
invalidates:
  - The app refuses a running factory stage in its own process as another codeaf window.
---

Opening a running factory stage in its crew attaches to the same conversation. Closing that view leaves the stage running; Stop and answers still reach it. Model, instructions, posture and spend limits remain the factory’s. Paused and resumed stages release old views so the resumed stage can be opened again. Stages held by a different process retain the existing refusal.
