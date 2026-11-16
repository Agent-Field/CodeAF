---
kind: added
title: a crew made on another computer shows in the desktop as one crew, read-only
pr: 2653
surface: [engine, app, docs]
invalidates:
  - "Was true: ChatRow.crew was declared but never set, so on a paired computer a crew's manager and members arrived as loose chats, and crews.list there was empty (seen live on two GCP machines, 2026-11-16). Is true now: every chats.list and chat.changed row carries crew, crew_name, crew_manager and crew_kind (\"factory\" for a factory item's crew), and hello says chats.crew."
  - "Was true: PROTOCOL.md said crew is omitted because relay rows carry no crew membership. Is true now: the holder seals the chat's crew into its directory record with every saved turn (field crew, relay header Codeaf-Chat-Crews: 1, sealed under its own associated data), so it survives the holder going offline. This computer's own teams store wins for any chat it lists."
  - "Was true: the vault team sync was the only way crews reached another computer. In codeaf app it ran only while pairing was still pending, and it merges crews in as editable ones. Is true now: the app does not use it for this. A crew from another computer is read-only here. It is not in crews.list, no crew verb acts on it, chats.send reaches its manager, and agents.take still moves one member as a single chat."
---

Crews travel only through a relay that answers Codeaf-Chat-Crews: 1. relay/hosted
needs a deploy before the hosted relay carries them; until then rows stay loose.
A rename or removal on the holder arrives with that chat's next saved turn.
docs/design/remote-read/DESIGN.md ("Crews on another computer") says why the
record carries this and not the vault team sync.
