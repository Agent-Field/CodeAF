---
kind: fixed
title: the desktop app restores answered receipts, quiet notices and turn-failure lines after a relaunch
pr: 2023
surface: [app]
invalidates:
  - "Was true: the app engine kept only permission-saved receipts in its durable history, so an answered ask's receipt, an engine notice and a turn's failure line existed only in the live event stream and vanished on relaunch. Is true now: the app persists answered (and allowed-once) receipts, quiet notices and turn-failure entries through the same sidecar the listing replays, so agents.entries restores them in order with their stable ids and timestamps."
  - "Was true: a stop footer, a provider-failure footer and a generic error entry were drawn live and never written to the sidecar. Is true now: each is kept as it is emitted, so its line survives the process."
---

The desktop app's `agents.entries` listing rebuilds a conversation from the
model transcript plus a sidecar of app-only footers. The answered receipt, the
engine's recorded quiet notices and a turn's failure entries were emitted live
but never written to that sidecar, so a clean quit and relaunch dropped them
while the question, answer and the assistant's own replies stayed. Each of the
three is now persisted as it is emitted, through the existing footer sidecar,
with the id and timestamp it already carried live. A notice the engine marks
`unrecorded` — memory's lines and a factory's progress — stays live-only, as
the contract says it must.
