---
kind: changed
title: chat keeps messages readable and folds operational work across every conversation view
pr: 1607
surface: [chat, engine, docs]
invalidates:
  - "Steering during streamed tool arguments left a discarded call visible and leaked the next response’s [update] marker. Consuming a correction now closes the response boundary, preserves its human update, and removes calls that never ran (#1609)."
  - "Task completion cards and batches stayed expanded and could not be dismissed. They now start as one row; /dismiss hides settled notifications and /dismiss undo restores them without changing task records or pending decisions."
  - "Manager team exchanges, failed calls, skills, and compaction could spill outside live activity. Operational work now shares the existing scrolling activity view, capped at three rows, with a failure mark and full detail behind disclosure."
  - "Later work or task receipts could demote a completed answer into raw Markdown. Explicit tool-free response boundaries and marked user updates now keep messages to the person readable in live and replayed conversations, including updates before more work."
  - "A nested node transcript bypassed folding and an opened workfold could omit non-caption details. Node transcripts now share task-room disclosure, retain their own expansion state, and expose the complete history when opened."
  - "Team wake instructions invited repeated status paragraphs. The manager now uses the existing exact no-change response when coordination produces nothing new to tell the person."
---

Closes the presentation gaps tracked in #1564, #1565, #1605, and #1609.
