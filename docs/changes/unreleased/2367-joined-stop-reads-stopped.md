---
kind: fixed
title: a joined task ended by its run's stop is reported stopped, not already finished
pr: 2367
surface: [chat, engine]
invalidates:
  - "A stop on a joined task that its run's stop had already ended answered that the task had already finished, whenever the joined stop reached the store after the run's stop. It answers that the task was already stopped with the run's own task, and already finished is said only for a task that landed or failed on its own."
---
