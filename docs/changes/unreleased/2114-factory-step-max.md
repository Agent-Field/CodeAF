---
kind: added
title: The person gives one conversation step of one factory job 1 to 3 tries
pr: 2114
surface: [engine]
invalidates:
  - "A factory job step's tries came only from the recipe. factory.job_step_edit now takes edit.max {step: 1..3} for a conversation step, shown as steps[].max on the job read."
---

The hello says `factory.job_step_edit_max`.
