---
kind: fixed
title: Desktop inline place creation picks a tint and restores focus
surface: [desktop, chat]
invalidates:
  - "The Home inline create tile defaulted to graphite or its parent's tint. It now selects the least-used of five tints in the active grid, with the Places 8f picker order."
  - "Escape only canceled from the name field and lost focus. It now cancels from the picker too and restores keyboard focus to the New place tile."
---

Creation keeps the existing engine write path and current Home parent. Blank
names and duplicate sibling names cannot submit; failed writes preserve the draft.
