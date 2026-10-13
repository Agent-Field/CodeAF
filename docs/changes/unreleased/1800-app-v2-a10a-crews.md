---
kind: added
title: App crew and member routes keep conversations and enforce manager and cap rules
pr: 1800
surface: [engine, chat, docs]
invalidates:
  - "App crew and member routes returned authenticated 501 stubs. A10a now implements proposal, creation, list/read/edit, replacement and membership routes over the native team store."
  - "The native team cap was a daily pool. App crews now keep a lifetime budget and member caps, stop work at a crossing and save one budget question."
---

The manager is replaced, never removed or moved, and the former manager stays
a member. Existing chats keep their transcript ids; a role reused in another
live crew gets another conversation. Metadata stays in teams.json's preserved
extension under its native lock, and proposals stay private and durable.

Producer hooks expose question paths, member-message admission and manager log
visibility, and budget asks to their owning turns/asks/Inbox lanes. Those lanes'
reserved routes are not implemented by this change. Role reads use an interface.
