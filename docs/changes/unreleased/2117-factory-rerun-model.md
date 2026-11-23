---
kind: added
title: Run again from here can name the model the step runs on
pr: 2117
surface: [engine]
invalidates:
  - "factory.job_step_rerun took only words and reran the step on its own model. It now also takes model, a models.list id the step runs on from then on, logged as Rerun from <step> on <model>."
---

The hello says `factory.job_step_rerun_model`.
