---
kind: fixed
title: codeaf app asks for a crew proposal beside the chat, so only the proposal's ask shows
pr: 1843
surface: [engine, remote]
invalidates:
  - "Was true: `crews.propose` sent the app's instruction (\"Propose a crew for the work in this conversation…\") as a real turn, so the person saw it as their own message, watched the model's JSON stream in and land as an answer, read both back from `agents.entries` and after a relaunch, and the model carried the exchange into every later turn; a press while the agent was working was refused busy, and at the day cap the refused turn's retry sent the instruction again. Is true now: the proposal is a side ask (`Agent.SideAsk` in internal/session, carried across the wire as `Conversation.SideAsk` with the welcome flag `sideAsk`) that reads the conversation as text on its own model, runs no turn, emits no `turn.*` or `entry.added`, and writes nothing to the journal but its spend; the person sees only the `crew_proposal` ask, or one quiet notice (id `crewprop-none-…`) when nothing readable came back, the ask failed or the day cap stood in the way. A press while the agent works is asked at once, a second press while one is out is answered by the first's card, and at the day cap the day cap's ask opens beside that notice."
  - "Was true: making the crew from the card left two receipts, a bare \"Made the crew · you · HH:MM\" beside the crew's formation fact, and keeping the agent alone said \"Kept it one agent\". Is true now: making the crew leaves the formation receipt alone, and keeping the agent says \"Kept as one agent · you · HH:MM\"."
---

An engine without the side ask refuses it rather than falling back to a turn,
and the app says so on the press.
