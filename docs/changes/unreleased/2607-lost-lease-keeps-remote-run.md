---
kind: fixed
title: A lost worker lease no longer cancels remote AgentField runs
pr: 2607
surface: [factory, docs]
invalidates:
  - "Was true: when a factory worker lost its lease for a reason other than withdrawal (an expired lease answering HTTP 409 or a heartbeat unable to reach the queue), job.drop called Runner.Stop, which marked the job stopped by a person and posted cancel-tree to the AgentField control plane. Is true now: only a person's Stop marks the lease stopped and posts cancel-tree (once); any other lease loss withdraws the local runner without cancelling the remote run, leaves the job re-queued rather than stopped by a person, and lets the next lease re-attach to the same remote run."
---
