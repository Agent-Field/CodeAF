---
kind: fixed
title: Task prompt facts promise a folder of their own rather than a copy
pr: 2391
surface: [chat]
invalidates:
  - "The chat prompt claimed every task gets a copy. Tasks get a folder of their own; Reference tasks and tasks on the home folder get an empty folder."
---

Keep the rule that the model never specifies the task's workspace method.
This corrects prompt wording only; task workspace behavior is unchanged.
