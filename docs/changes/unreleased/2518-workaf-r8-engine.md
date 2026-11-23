---
kind: fixed
title: A run that finished just before a quit reads finished, and a run that changed nothing says so
pr: 2518
surface: [factory, engine]
invalidates:
  - "An agentfield step whose run had already ended when its runner went away was an ordinary interrupted step (machine_lost, retried). Now a run that ended while its step was still running, begun no earlier than that round, is queued with no failure, and the round a restart picks up (factoryrun.Job.Continuing) reads the run's end as the step's. It does not dispatch a second run. Only a fresh round starts a new one."
  - "afOutcomeNote took the newest control-plane note as how a run ended. It now ranks notes by meaning (no pull request opened or empty change, then any failure, then progress) through factory.AgentFieldTellingNote, so a tidy-up note written after the decision is never the headline."
  - "A run that changed nothing ended shipped with no pull request and no reason. Its step now ends done with output factory.AgentFieldNothingToChange, and the job's state_presentation reads title \"Nothing to change\", detail \"Nothing to change: the run made no changes, so no pull request was opened.\" The job state stays done, and failure stays null."
  - "follow's drain after a run's end ran on context.Background for up to 15 s, which held a quitting engine's local factory for appLocalStopWait (10 s). The drain is now bounded by the reading's context."
  - "revive said \"resumed\" for every job a runner took back. It no longer does when the job only waits on an AgentField approval or has no step left. The readiness line now names the commit by 7 characters instead of 40."
---
