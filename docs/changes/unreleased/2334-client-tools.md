---
kind: added
title: Apps can attach their own tools to a conversation, and collections keep blank conversations
pr: 2334
surface: [app, chat]
invalidates:
  - A never-spoken conversation was removed at launch unless a team referenced it; a conversation any collection references is now kept as well.
---

`codeaf app` clients can attach named sets of tools to a conversation with
`agents.tools_set`. The model sees them after every built-in tool, for as long
as that client stays connected. Each call reaches the client as one
`agents.tool_call` event, and the client answers it with `agents.tool_result`.
A disconnect, the deadline (30 seconds by default, at most 120) or the end of the
turn answers the model with an error that says the outcome is unknown. Late or
repeated answers are ignored. A conversation holds at most 4 sets and 64 tools;
built-in tool names are refused, and child tasks never receive client tools. The
hello carries `agents.client_tools`. Conversations held by a remote engine are
refused for now.

Launch cleanup, the blank-chat sweep and the background sweep now keep a
never-spoken conversation that any collection references. An unreadable
collections database keeps it too. Opening such a conversation by id after a
restart restores its saved details.
