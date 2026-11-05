---
kind: added
title: Client tools reach conversations held by another codeaf or on another computer
pr: 2417
surface: [app, remote, chat]
invalidates:
  - The app answered bad_scope to agents.tools_set for a conversation another codeaf host held or one held on another computer; it now forwards the tools when the holder supports it and stores them for a chat held elsewhere.
---

**Client tools across the remote door.** When the app joins a conversation that
another codeaf host already holds, `agents.tools_set` sends the agent's sets to
that holder. Its calls come back as `agents.tool_call` on the app connection
that set them, and `agents.tool_result` answers go back the same way. The app
pushes the sets again every time it joins the conversation, and a roaming
connection re-sends them after a redial.

A connection that leaves loses its sets, and its pending calls end at once with
the existing disconnected answer. A call nobody answers still ends at its wait.
Two connections on one conversation keep separate sets and never receive each
other's calls.

A chat held on another computer keeps its sets until it runs here again. The app
hello adds `agents.client_tools.remote`. A holder from an older build is still
answered with `bad_scope`, and the remote protocol version is unchanged.
