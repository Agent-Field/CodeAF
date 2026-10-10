---
kind: changed
title: Desktop chrome uses the shared design tokens
surface: [desktop, chat]
invalidates:
  - "Desktop chrome kept a separate legacy control palette and focus outline. It now uses shared controls, the keyboard ring and halo, theme surfaces, and the design's disabled opacity."
  - "History and preview added-line counts kept separate fixed colors. Both now resolve the theme's success color."
---

Remove unused legacy tokens after migrating shared styles. Retain tokens with
consumers in feature styles until their owning lanes migrate those consumers.
