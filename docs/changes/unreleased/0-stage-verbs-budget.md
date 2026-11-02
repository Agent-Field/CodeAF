---
kind: fixed
title: Factory steps carry only the answer fields they can use
pr: 0
surface: [engine]
invalidates:
  - "Every factory step's stage verbs carried the pull request repair report, which only the step following a pull request can write. That step alone is offered it now."
---

Each factory step's tool words are sent with every model call. Steps that don't
follow a pull request no longer carry the repair report, so their calls cost less.
