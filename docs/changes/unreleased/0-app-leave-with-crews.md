---
kind: fixed
title: The app can quit while a crew member is still opening
pr: 0
surface: [engine, docs]
invalidates:
  - "Optional file cleanup on app quit waited on every conversation's gate, including a crew member still opening. Cleanup now keeps a busy member's files and lets the engine leave."
  - "Closing the desktop input pipe armed the leave bound only after acquiring the backend lock. It now starts the same bounded leave as the first SIGTERM before waiting for that lock."
---

Held conversations still record their quit and resume facts, interrupt running
turns and flush their records before release. Settled conversations keep the
existing attachment and blank-conversation cleanup. Terminal behavior and the
overall shutdown allowance are unchanged.
