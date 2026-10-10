---
kind: fixed
title: Desktop pane scroll persistence uses a 500ms throttle
surface: [desktop, docs]
invalidates:
  - "Window-local pane scroll persistence wrote after 300ms. It now batches continuous scrolling every 500ms and flushes pending changes when leaving the window."
---

Returning to a pane or reloading keeps a mid-transcript position; a pane left
following the bottom restores to the current end. Existing window-local memory
and pane restoration remain the single controller, with deterministic tests
for the throttle and delayed-replay restoration rules.
