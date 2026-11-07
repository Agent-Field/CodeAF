---
kind: fixed
title: an app-run engine hands every credential a door gave it to the app to keep
pr: 2500
surface: [factory, engine]
invalidates:
  - "Was true: in codeaf app the AgentField key and node secret (factory.agentfield_connect) and a Jira token were held in memory only and lost at every desk launch, and keys.set refused them ('codeaf cannot connect agentfield with a key'). Is true now: each door keeps them with AppCredentials.Keep, the app hears credential.changed {provider, kept, key} once to keep in its secure store, and keys.set takes github, jira, agentfield and agentfield-node back at the next launch without echoing them; Forget sends {provider, kept:false}."
  - "Was true: only the desk kept a pasted GitHub token, through its own setup call. Is true now: the engine also sends it as credential.changed (a gh-login connection is still held for the launch only)."
---
