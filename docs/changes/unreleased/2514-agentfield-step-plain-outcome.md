---
kind: fixed
title: an AgentField step that did not succeed says why in plain words, and its job file says failed
pr: 2514
surface: [factory, engine]
invalidates:
  - "Was true: an agentfield step's failure read 'the AgentField run <node.reasoner> ended: <raw report>' and a finished one 'the AgentField run <node.reasoner> finished and opened <link>'. Is true now: it reads '<step label> did not succeed: <why>' and 'finished and opened <link>'; the why is the run's last note or summary read by its shape (a leading grade such as OVERALL: FAIL dropped, cut before any quoted command, 'No pull request: X' read 'X, so no pull request was opened'); the raw report is the failed step's output."
  - "Was true: the control plane's outcome note (for example 'No pull request: the build did not pass verification') never reached the job. Is true now: the engine reads GET /api/v1/executions/<id>/notes and stores the outcome as failure.summary, which state_presentation.detail now prefers over failure.detail."
  - "Was true: a failed job's file said State \"new\". Is true now: it says State \"failed\" on disk and is read back in memory as new with its failure; an older build reading such a file sees an unknown state."
  - "Was true: a step revived after a restart re-stamped its start time and its chat said '<step> started' again under 'resumed'. Is true now: it keeps its start and says started once."
  - "Was true: Follow PR and the merge gate said 'verified' ('its verified changes must be approved'). Is true now: they say 'checked'."
---

The finding behind this is WorkAF live e2e rounds 6 and 7: the Brief, Afi and every
SWE-AF chat printed the reasoner's dotted id and its raw report.
