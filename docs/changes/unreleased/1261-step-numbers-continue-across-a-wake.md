---
kind: fixed
title: a task's step numbers continue across a wake
pr: 1261
surface: [chat, engine]
invalidates:
  - "A run worker numbered its steps from one each time it ran. A task that waited on its children and was launched again recorded a second step 1, so its page drew `1` to `5` and then `1` to `3`, and the live step was called by a number an earlier row already wore."
  - "A worker now starts from the last number the task's record holds, so a task's step numbers are counted over the task and never repeat. The step cap and the worker's own report still count THIS run of the worker only: a task that came back has a fresh count against the cap, and its ending line reports the steps of that run."
---

The record's shape is unchanged; a record written before this keeps the numbers it has.
