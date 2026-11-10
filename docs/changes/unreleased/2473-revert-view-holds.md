---
kind: fixed
title: Desktop chats on screen stay held again, so another process cannot run a turn in one unseen
pr: 2473
surface: [app, engine, docs]
invalidates:
  - "After #2472 the desktop engine let an open chat go once it was idle and outside the three most recently used, so a terminal codeaf on the same home could run a turn in a chat the window still showed without the window seeing it. An open view keeps its conversation and hold until it is closed again, as before #2472."
---

This reverts #2472. Per-chat file handles held by the engine (#2471) are
unbounded again until a fix keeps both the hold and a bound.
