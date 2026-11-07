---
kind: fixed
title: A failed or rejected factory job stays ended, and a step's questions and outputs stay on the job
pr: 2500
surface: [factory, engine, app]
invalidates:
  - "A job whose run failed went back to the state a routing rule's start: auto picks up, and the rule started the whole run again by itself, seconds later and at the next engine start. A rule now starts only a job that never ran (factory.EverRan); a failed job waits in Needs you for a person's Try again or Run again."
  - "A no to an approval with nothing before it to send back to stopped the job, leaving it in Needs you offering Carry on. It now ends the job as rejected: done, closed here with the person's words (Item.Rejection), the run's waiting step closed, out of Needs you. A new door, factory.job_reject (POST /v1/jobs/{id}/reject, can.reject), rejects any waiting approval the same way."
  - "A job a person stopped read needs_you and counted in Needs you. It now reads backlog, with its stop on it."
  - "When the job's manager answered a step's question, nothing but a log line and a note recorded it, and the Brief read the note as the person's own request. Every manager answer is now kept in the job's answered questions with with: manager, the Brief reads it as manager_answers, and it is never a person request."
  - "The manager answered every step question first in every mode. A job in Manual now sends every step question to its person, and a question the step marks as needing a person (the ask tool's judgement kind, Asked.Person) goes to the person in every mode."
  - "A step that reported outputs and then failed lost them with the failure. Its result now stays on its step."
  - "An executor could not tell a person's Stop from the engine letting go of a job: both cancelled the step's context with no cause. A stop now cancels it with factory.ErrPersonStopped (run.StoppedHere), so work running elsewhere can be cancelled only by a stop."
---

From a live AgentField run on 2026-11-07: a failed step restarted its whole build twice by
itself, a rejected plan sat in Needs you offering to carry on, and the manager answered for
the person invisibly.
