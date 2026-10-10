---
kind: changed
title: Desktop queued rows edit on text click and offer immediate delivery
surface: [desktop, chat]
invalidates:
  - "Queued message text did not open its editor, and rows had no context menu or Send now action. Text now edits inline, and right-click or Shift+F10 offers Edit, bounded moves, Send now and Remove."
---

Touch pointers keep row actions visible and disable dragging. Queue changes
continue through the canonical engine, including immediate delivery.
