---
kind: fixed
title: codeaf app opens its process half again once a key arrives after a failed open
pr: 0
surface: [engine]
invalidates:
  - "`codeaf app` kept the first open of its process half for the life of the process, failed or not, so an open that ran before `keys.set` (the hello's warm, or a `models.list` sent before the key was handed over) answered every later `models.list`, `agents.create` turn and model switch with `OPENROUTER_API_KEY is required`. The app's door now opens a failed process half again on the next call, so the call after `keys.set` uses the key; a successful open is still never replaced, and every other door keeps its first answer."
  - "`keys.set` updated the process half only once a conversation had been assembled, so a key handed over while the process half was open but no conversation was held reached nothing. It now updates whatever process half is open, whoever opened it."
---

Was true: on a first launch the app's first `models.list` could reach the engine
before its key did, and the first message's turn ended `ok:false` with
the missing-key error the moment it was created.

Is true now: the missing-key error answers only the calls made before the key
arrives; the next call opens with it.
