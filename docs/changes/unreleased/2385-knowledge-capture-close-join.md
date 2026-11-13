---
kind: fixed
title: Closing chats joins knowledge capture and cancelled job learning
pr: 2385
surface: [chat, engine, factory]
invalidates:
  - "An unbounded knowledge join could hang application quit."
  - "Close waited two seconds for knowledge capture and then returned, so a final scan or a cancelled job compiler could keep writing after close."
---

Close first cancels current and retired scanners and standalone or
already-cancelled job compilers, then waits up to 10 seconds total. On a miss it
logs each unfinished writer's kind, hashed journal/profile identities and elapsed
wait, then returns. An in-progress SQLite write can finish later atomically.
Active job compilation retains its existing lifetime across stage closes.
Accounting and admission rules are unchanged. The live budget test no longer polls a
SQLite journal for up to ten seconds after process shutdown.
