---
kind: fixed
title: Desktop text keeps its design colours inside replies and warning toasts
surface: [desktop, chat]
invalidates:
  - "Markdown's accent link rule overrode link chip titles, and a global warning rule coloured toast text amber. Chip titles and warning toast messages now retain full ink."
  - "The contrast waiver omitted shared keyboard hints, closed-but-running rail titles and neighbouring filmstrip labels. It now names those design-muted selectors without waiving other axe rules."
---

Decision card bodies and comparison tables accept keyboard focus so the same
accessibility sweeps can reach their scrollable content.
