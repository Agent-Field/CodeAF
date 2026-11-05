---
kind: fixed
title: Closing chats joins knowledge capture and cancelled job learning
pr: 2385
surface: [chat, engine, factory]
invalidates:
  - "Close waited two seconds for knowledge capture and then returned, so a final scan or a cancelled job compiler could keep writing after close."
---

Close joins current and retired scanners. Standalone and already-cancelled job
compilers are joined too; active job compilation retains its existing lifetime
across stage closes. Accounting, admission and cancellation rules are unchanged.
A stalled filesystem can delay close. The live budget test no longer polls a
SQLite journal for up to ten seconds after process shutdown.
