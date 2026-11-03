---
kind: added
title: Collections hold Factory jobs and keep blank conversations, and apps attach client tools
pr: 2336
surface: [app, chat]
invalidates:
  - The collections database becomes schema 7 on its first successful write; an engine built before this change refuses it, leaving collections unavailable there and data intact.
  - A never-spoken conversation was removed at launch unless a team referenced it; a conversation any collection references is now kept as well.
---

**Factory jobs in collections.** A membership can name a Factory job: kind
`factory_job` with its `factory`. `codeaf collections add|remove|find` take
`--factory`. In the app, `collections.open` shows each job's title, status and
repository from one bounded, briefly shared read per Factory; a job that is gone
or unreadable keeps its row and says so. One job may sit in several collections.

**Client tools.** An app attaches sets of tools to one conversation
(`agents.tools_set`, hello feature `agents.client_tools`). They follow the
built-in tools while that connection lasts. The engine sends every call as an
`agents.tool_call` event and waits for `agents.tool_result`; a lost connection,
the turn ending, or the deadline (default 30 s, ceiling 120 s) gives the model
an error saying the outcome is unknown, and a late or second answer is dropped.
Limits: 4 sets, 64 tools, no built-in names, never inherited by child tasks.
Remote-held conversations are refused for now.

**Kept conversations.** Every cleanup path (launch, blank-chat sweep, background
sweep) now spares a never-spoken conversation that some collection holds, and an
unreadable collections file counts as holding it. After a restart, opening it
by id brings back its saved details.
