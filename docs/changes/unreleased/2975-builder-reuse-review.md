---
kind: fixed
title: Continue building an app after accepting record reuse
pr: 2975
surface: [app, engine]
invalidates:
  - "A saved plan used to appear ready to try before the builder wrote its screens. The revised outline now stays in Plan and offers Write it."
  - "An unbound common type could ask for permission to share records that did not exist. The builder now checks it as a definition issue and reserves consent questions for existing records."
---

A common type starter describes the list the app can start. An existing
record source gets its own reuse offer. When Check needs a reuse decision,
Desk shows the question and the choices together in the outline. Keep
becomes available after the engine checks the written screens.
