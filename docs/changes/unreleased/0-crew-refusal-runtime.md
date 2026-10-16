---
kind: fixed
title: A running task's pinned role recovers on a new key or credit; a refusal names the key it carried
pr: 0
surface: [chat, engine]
invalidates:
  - "A task's own record of a refused key or empty account (taskCrew failed/bad/gone/broke and a decision.Stopped set from them) held the pinned role for the rest of the task, even after route health lifted the refusal. Now the next seat call checks config.CrewRefusalApplies and the connection the conversation's pool would actually send, and lets go only the auth/payment refusals those facts answer; rate limits, gone routes, caps, spend stops and unanswered refusals on the same provider stand, and nothing is sent because a refusal was lifted."
  - "A crew seat's refusal was written to the router log with no credential print. It is now stamped with the print of the bearer the refused request's own Authorization header was written from: provider.Client.ObserveRequestCredential reports a one-way provider.CredentialDigest per attempt, at the line that snapshots the key for the header, and the seat keeps the last attempt's, logged with config.LogCrewRouteSent. Neither the pool's account (a key handed to the live adapter after the door chose it travels instead) nor the settings are read for it; a key rotated before or while the call was in flight is not blamed."
  - "config.CrewCredentialDomain(id) is the domain crew prints are hashed under, and crew prints are provider.CredentialDigest of it; the bytes are unchanged, so prints already recorded still match."
  - "A Codex sign-in is never printed at dispatch: its transport replaces the header with tokens it loads and refreshes per request, so its refusals stay unprinted and are lifted only by a recorded sign-in after them. A request carrying its own APIKeyOverride is unprinted too."
  - "TaskCrewRecord carries FailedSent (failure time, provider, credential print per failed send) so a carried-on task keeps those facts; older checkpoints have none and their auth/payment refusals stay held."
---
