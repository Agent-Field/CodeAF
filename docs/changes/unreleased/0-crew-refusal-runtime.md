---
kind: fixed
title: A running task's pinned role recovers on a new key or credit; a refusal names the key it carried
pr: 0
surface: [chat, engine]
invalidates:
  - "A task's own record of a refused key or empty account (taskCrew failed/bad/gone/broke and a decision.Stopped set from them) held the pinned role for the rest of the task, even after route health lifted the refusal. Now the next seat call checks config.CrewRefusalApplies and the connection the conversation's pool would actually send, and lets go only the auth/payment refusals those facts answer; rate limits, gone routes, caps, spend stops and unanswered refusals on the same provider stand, and nothing is sent because a refusal was lifted."
  - "A crew seat's refusal was written to the router log with no credential print. The client door now tells the seat the print of the exact connection its adapter carries (the pool's account, not the settings) before the request leaves, and the refusal is logged with config.LogCrewRouteSent. A key rotated while the call was in flight is not blamed."
  - "A Codex sign-in is never printed at dispatch: its transport loads and refreshes tokens per request, so its refusals stay unprinted and are lifted only by a recorded sign-in after them."
  - "TaskCrewRecord carries FailedSent (failure time, provider, credential print per failed send) so a carried-on task keeps those facts; older checkpoints have none and their auth/payment refusals stay held."
---
