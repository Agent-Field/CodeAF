---
kind: fixed
title: Finished shell calls keep their background processes after a late cancellation
pr: 0
surface: [chat, engine]
invalidates:
  - "A cancellation watcher could kill background processes after their shell call had already finished. A completed call now takes precedence over a late cancellation."
---

When a shell call's completion and cancellation are both ready, the cancellation
watcher checks completion before signalling the process group. Commands that are
still running retain their existing cancellation behavior.

Carries forward santoshkumarradha's completed-call cancellation fix and regression
test from #1738. Pairing and moving chats already shipped separately; this change
does not reintroduce the old relay or its model fallback and skill import policies.
