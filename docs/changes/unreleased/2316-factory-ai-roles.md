---
kind: fixed
title: Step and My pipeline AI count as Factory AI in daily spending
pr: 2316
surface: [engine, factory]
invalidates:
  - "Was true: step_ai and my_pipeline_ai spending without a job counted under your chats. It now counts under Factory AI."
---

Step AI and My pipeline AI usage without a job now counts in `factory_ai_usd`,
not under your chats. Usage attached to a job still belongs to that job.
