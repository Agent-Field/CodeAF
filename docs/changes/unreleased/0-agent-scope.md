---
kind: added
title: A conversation can be told only about its own work (agents.set_scope)
pr: 0
surface: [app, engine]
invalidates:
  - "Was true: every conversation in a folder was told about the other conversations' landed and running work there, and its tasks tool listed them. Is true now: a client can set a conversation's saved scope to conversation so its turns and tasks tool show only its own work; project remains the default and restores the earlier behavior."
---

`agents.set_scope {id, scope}` stores `project` or `conversation` and answers the
stored word. Engines serving it announce `agents.scope`. The choice survives a
restart and takes effect on the next turn or tasks call without opening,
warming or interrupting the conversation. Its folder, transcript, title, model
and tasks stay as they were. A lent conversation or one without a local folder
is refused.

The person's task list, task nodes and the write-safety checks keep their
existing behavior.
