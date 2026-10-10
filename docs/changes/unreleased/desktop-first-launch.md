---
kind: fixed
title: Desktop first launch matches the design and creates folders atomically
surface: [desktop, chat]
invalidates:
  - "First launch separated the explainer from its title and used the root's four-column grid. It now uses Places 8e's title grouping and three-column grid."
  - "Opening a folder made a named place and then added its source separately. It now calls the canonical from-folder route before navigation, so a refused source cannot leave a half-created place."
---

The native folder tile uses the folder-open glyph. The existing first-place
matching-tab offer remains the one-click move door after Go to.
