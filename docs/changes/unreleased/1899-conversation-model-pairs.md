---
kind: changed
title: Conversation-local main and fallback model selection
pr: 1899
surface: [engine, chat, remote]
invalidates:
  - "Desktop chats exposed Fixed/Automatic controls and factory-wide model changes. Each chat now owns Main and Fallback, with actual execution identity and durable switch notices."
---

Desktop chats now own one Main/Fallback pair rather than an exposed Fixed/Automatic
policy and a job-wide model preference. A manager no longer displays its worker's
identity. Each request starts with Main, can use only its selected Fallback, and
returns to Main for the next request. Changes never cancel an active request.

Typed journal notices record configuration changes and actual model transitions,
including fallback use and return to Main. These facts do not enter model context.
The existing Settings model/fallback rows supply defaults; saved conversation
choices remain independent across bare, team, task, factory-manager and step chats.

Invalidates: desktop Fixed/Automatic controls and the claim that changing a model
in a factory step changes the whole job. Terminal routing and launch restrictions
retain their existing behavior when no desktop conversation pair is configured.
