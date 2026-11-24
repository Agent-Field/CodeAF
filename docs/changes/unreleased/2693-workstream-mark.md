---
kind: added
title: a workstream's conversation stays out of the Library and has no automation tool
pr: 2693
surface: [app, engine, chat, docs]
invalidates:
  - "Was true: every saved conversation, including a desk workstream's Afi and chats, was captured into the knowledge Library once the Library was switched on, and offered the `automation` tool when a store was behind it. Is true now: `agents.set_workstream {id, workstream}` (hello word `agents.workstream`) marks a conversation as a workstream's, persisted in its meta; a marked conversation, or one carrying a `Config.Workstream` door, is never captured or used as a compiler source, has no `automation` tool, and its page says what it makes goes to the workstream's Files."
---
