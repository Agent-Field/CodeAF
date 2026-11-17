---
kind: internal
title: AgentField runs as factory steps now live on the general factory branch
pr: 2503
surface: [factory, engine]
invalidates:
  - "AgentField runs as factory steps (the agentfield step, run_on jobs, factory.job_answer and factory.job_reject on a run's pause, codeaf exec --events/--steer) lived only on proto/af-watch, apart from the general factory. They are now on factory/general-intake with it, and that one branch carries both."
---
