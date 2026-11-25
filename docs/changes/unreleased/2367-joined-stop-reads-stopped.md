---
kind: fixed
title: a joined task ended by its run's stop is reported stopped, not already finished
pr: 2367
surface: [chat, engine]
invalidates:
  - "A stop on a joined task that its run's stop had already ended answered that the task had already finished, whenever the joined stop reached the store after the run's stop, and a joined stop that lost the race to the run's stop came back as an error. Both now answer that the task was already stopped, with the run's own task when the run's stop ended it."
---
