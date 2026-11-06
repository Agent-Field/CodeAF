---
kind: fixed
title: Desktop chats shown in a session no longer stay open in the engine until the app quits
pr: 2472
surface: [app, engine, docs]
invalidates:
  - "A chat opened for viewing in the desktop app kept its conversation and relay hold until it was explicitly closed. Only the three most recently used open chats keep theirs now; an older open chat idle for ten minutes is let go like a background chat and opens again on its next read or send."
---

The window opens every chat it shows and does not say when it stops showing
one, so each chat shown in a session stayed held with its journal, its presence
watch (about six file handles on macOS) and its timers. The idle sweep now keeps
at most three open views held; running turns, waiting questions, crews, kept
services and explicit closes are unchanged.
