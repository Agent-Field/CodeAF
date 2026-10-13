---
kind: added
title: App tasks and quick tasks use engine clocks, receipts, Undo and promotion
pr: 1800
surface: [chat, engine, docs]
invalidates:
  - "App task and quick-task routes returned 501. A5 now reads the canonical task tree and page and serves proposal, note, brief, pause, stop, model and all five your-call actions."
  - "Quick tasks recorded no original file bytes or hashes and had no Undo or size enforcement. The engine now captures each write before execution, keeps restartable receipts, checks all after hashes before Undo, and asks before file 31 or any shell call, including tests."
  - "An irreversible proposal could receive an admission clock. Such proposals and quick-task promotions now wait for an explicit answer; typing holds only the selected task."
---

Task and quick-task ids include their agent. Their additive version fields
support expected-state mutations. Quick checkpoints carry file bytes privately;
only paths, hashes and line changes cross the app wire. Promotion continues in
the same folder after the quick worker stops, preserving its in-place edits.
Graph tasks use the saved per-task cap (default $2), and note read receipts come
from the canonical steering record entering a model request. Native plan tasks
keep their own writers and omit an unknown note-read step. A5 uses the shared
launch-local POST replay door and standing engine task events.
