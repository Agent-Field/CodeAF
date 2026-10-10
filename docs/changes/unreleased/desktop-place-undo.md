---
kind: fixed
title: Desktop place actions share the window structural undo stack
surface: [desktop, chat]
invalidates:
  - "Desktop places and tabs kept separate undo stacks. They now share one per-window stack capped at twenty structural actions."
  - "Closing a place could not be undone after its toast expired. It now registers the canonical reopen operation in the same stack."
---

Place mutations register exact engine receipts alongside tab inverses. Keyboard
Undo follows structural action order instead of preferring the latest visible
toast. Text fields keep their native Undo. Toast Undo consumes its own step once,
including when it races the keyboard. Connection failures retain authority for
an explicit retry; engine refusals discard unavailable inverses.
