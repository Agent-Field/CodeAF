---
kind: fixed
title: An AgentField job started on a yes can merge; the editor gives an AgentField step its input
pr: 2522
surface: [engine, factory, chat]
invalidates:
  - "A job whose only step ran on AgentField, started on a person's yes to 'Start this job?' (a route that asks first), was shipped down the native publish path when its worker reported done, because that yes is an approval in the record and the worker's done read it as consent to publish. The job gained the factory's Follow PR step and a CI follow-up, stopped reading as one whose pull request a run delivered, never recorded the head the run left, and its Merge card said 'no approved commit is recorded for this job' for good. A job that runs elsewhere (factory.RunsElsewhere) now ends as its run did whatever the record holds, and factory.RunDeliveredPR leaves the Follow PR step out of the question, so a job shipped the old way records its head at the next GitHub read."
  - "The chat manual said the app's pipeline editor adds the same agentfield step as the file, but the editor gave the step only a name and a reasoner. Its input is now one line per field (factory-agentfield.md, 'give an AgentField step its input in the app'); the engine's add_step already took any input, which TestAWholeAgentFieldPipelineIsMadeByTheEditorsEdits now proves for the tenth round's whole pipeline."
---

From the tenth live WorkAF round (2026-11-08): the SWE-AF job's Merge card for PR #325 read "No approved commit is recorded for this job" and the SWE-AF pipeline had to be set through the engine. The r6 fix held only for jobs started from the backlog, which write no approval.
