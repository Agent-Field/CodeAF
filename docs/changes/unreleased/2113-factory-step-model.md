---
kind: added
title: The person picks the model one step of one factory job runs on
pr: 2113
surface: [engine]
invalidates:
  - "A factory job ran every conversation step on the job's one model. factory.job_step_edit now takes edit.model {step: model id}, the job read shows steps[].use_model, and that step runs on the pick with automatic selection off."
---

The hello says `factory.job_step_edit_model`. The engine checks each id against
`models.list`. A worker's report never sends the pick; the factory keeps it
through a runner's reshape.
