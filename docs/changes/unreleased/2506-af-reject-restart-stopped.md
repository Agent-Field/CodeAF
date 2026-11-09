---
kind: fixed
title: A rejected AgentField run ends Rejected, a restart is not a retry, a stopped job offers no Resume
pr: 2506
surface: [factory, engine]
invalidates:
  - "Was true: a Reject of an AgentField step's pause could end the job as a failed step (failure code tool_error, \"the AgentField run cancelled: approval_rejected: …\", state_presentation \"Step failed\" with Try again) when the runner's reading of the control plane's cancel reached the factory before its acknowledgement of carrying the Reject. Is true now: lease.Desk.Apply ends the job Rejected with the person's words whenever a run of it ended on a rejected decision recorded on one of that run's own pauses, or on the control plane's `approval_rejected: <words>` cancel reason (a no given on its own page), whichever arrives first (factory.AgentFieldRejectedEnd). A run that failed on its own is still a failed step."
  - "Was true: a runner lost (an engine restart) while its agentfield step's run went on at the control plane stamped machine_lost, and recovery read \"round 1 · interrupted · trying build again · Automatic retry 1 of 3\", spending the retry budget. Is true now: such a job is queued again with no failure, no recovery and its round unchanged, as when a runner lets go, and the returning runner follows the same run (factory.AgentFieldRunLive). A run that already ended is still an interrupted step."
  - "Was true: a job a person stopped had can.resume true, though Resume refuses it with \"is not paused\". Is true now: a stopped job offers start_now, run_again, run_as_new (and the rerun_step control), never resume. TRY.md's \"offers Start only\" was wrong; the manual's list stands."
---
