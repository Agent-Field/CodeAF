---
kind: added
title: senior-dev can be told something while it works, from its page or the chat, until it hands in
pr: 1689
surface: [engine, chat, docs]
invalidates:
  - "senior-dev read nothing after its brief: its page's box said `senior-dev reads no messages — say it to main`. The box now says `Tell senior-dev something…` and sends the line, which senior-dev's model reads before its next model call; before it starts, the box says `senior-dev has not started reading messages yet`."
  - "The chat's `tasks` say to a running senior-dev task was refused (or, before that, stored unread). It is now delivered through the program's inbox and marked had only when senior-dev says it heard it; after senior-dev hands in it is refused with `senior-dev reads no more messages (it has handed in its work, …)`, or `it has stopped working` when the run stops without handing in."
  - "A delegate program had no road in from codeaf while it ran; stdin stays closed. A program that sets `Delegate.Listens` is started with `CODEAF_INBOX`, says `accepts: [\"messages\"]` in its hello, and answers with `heard` and `inbox` records (docs/design/delegate/PROTOCOL.md §5a)."
  - "senior-dev's step loop had a between-steps reminder hook nothing used. It now carries the messages, and each one is kept in `.senior-dev/steering.md`; the newest 8 KB stays pinned beside the brief when history is summarized, including a capacity rebuild."
---
