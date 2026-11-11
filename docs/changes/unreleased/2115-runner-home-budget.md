---
kind: fixed
title: A factory runner on its own codeaf home runs steps instead of stopping on an unread budget
pr: 2115
surface: [engine]
invalidates:
  - "A step's live spend meter read the job's budget from the runner's own factory folder, so a runner whose home was not the factory's stopped every step with \"job budget could not be read\". It now reads and books on the worker's mirror of the leased job, and the worker's report carries the spend to the factory once."
---
