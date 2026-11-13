---
kind: fixed
title: Keep new and returning chats visible while conversation setup finishes
pr: 1946
surface: [chat, engine, docs]
invalidates:
  - "A worker joining more than twenty seconds after its conversation opened could stay idle until a second message; its own start now wakes it through the ongoing traffic watch."
  - "A new chat showed a separate opening screen before its ordinary conversation view; the ordinary view now shows its first message while setup continues."
  - "Returning to an open chat cleared its displayed history while waiting for the engine; its last displayed words now remain visible until refreshed history replaces them."
---

The worker reads its manager's initial brief once, even when conversation setup
delays membership beyond the short startup watch. Existing auto-wake settings,
start identity checks, delivery cursors, and execution limits still apply.

New chats use the ordinary conversation view while opening. Drafting a follow-up
does not cancel the first message, and opening failures preserve unsent words.
Returning chats retain their last rendered transcript rows as a temporary preview;
the engine's atomic replay replaces those rows instead of duplicating them.
