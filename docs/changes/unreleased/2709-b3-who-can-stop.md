---
kind: changed
title: only a task's owner or whoever looks after the workstream stops or moves it
pr: 2709
surface: [engine, factory]
invalidates:
  - "Was true: anyone who could edit an organization workstream could stop any of its tasks or move it to another workstream. Is true now: Stop (`state` `stopped`) and `move_to` are the task owner's or whoever looks after the workstream's (its steward, or full access); anyone else gets 403 \"only the task's owner or whoever looks after this workstream can stop or move it\", and the desk leaves both out of their More."
---
