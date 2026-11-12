---
kind: added
title: Apps can attach their own tools to a conversation and tell it only about its own work
pr: 2613
surface: [app, engine, remote, chat]
invalidates:
  - "Was true: every conversation in a folder was told about the other conversations' landed and running work there, and its tasks tool listed them. Is true now: a client can set a conversation's saved scope to conversation so its turns and tasks tool show only its own work; project remains the default and keeps the earlier behavior."
---

**Client tools.** `codeaf app` clients can attach named sets of tools to a
conversation with `agents.tools_set`. The model sees them after every built-in
tool, for as long as that client stays connected. Each call reaches the client
as one `agents.tool_call` event, and the client answers it with
`agents.tool_result`. A disconnect, the wait (30 seconds by default, at most
120) or the end of the turn answers the model with an error that says the
outcome is unknown. Late or repeated answers are ignored. A conversation holds
at most 4 sets and 64 tools; built-in tool names are refused, and child tasks
never receive client tools. The hello carries `agents.client_tools`.

`agents.tool_call` carries an optional `source_message_id`: the durable id of
the message the running turn answers, the same string as `Entry.message_id` on
that message's chat entry. It is the same after an engine restart and on a
replayed call; turns the engine starts itself carry none. The hello feature
`agents.client_tools.source` is advertised with `agents.client_tools`.

**Across the remote door.** When the app joins a conversation that another
codeaf host already holds, `agents.tools_set` sends the agent's sets to that
holder. Its calls come back on the app connection that set them, and answers go
back the same way. A connection that leaves loses its sets, and its pending
calls end at once. Two connections on one conversation keep separate sets. A
chat held on another computer keeps its sets until it runs here again. The app
hello adds `agents.client_tools.remote`. A holder from an older build is
answered with `bad_scope`, and the remote protocol version is unchanged.

**Conversation scope.** `agents.set_scope {id, scope}` stores `project` or
`conversation` and answers the stored word. Engines serving it announce
`agents.scope`. The choice survives a restart and takes effect on the next turn
or tasks call without opening, warming or interrupting the conversation. A lent
conversation or one without a local folder is refused. The person's task list,
task nodes and the write-safety checks keep their existing behavior.

This carries #2334, #2346, #2360, #2417 and #2462 from the closed integration
pull request #2306. Nothing specific to Workstreams came with it: the desktop
app keeps Workstreams itself and uses these two generic pieces.
