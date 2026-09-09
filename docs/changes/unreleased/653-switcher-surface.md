---
kind: changed
title: Separate the conversation switcher from the page beneath it
pr: 653
surface: [chat]
invalidates:
  - "Unselected switcher rows inherited the transcript background. The floating card now has its own theme-aware ground, with distinct selected and hovered rows and inner vertical padding when space permits."
  - "The keyboard destination relied on color and bold weight. A persistent greater-than marker now names it even without color, independently of the pointer's hover marker."
---

The card keeps its rounded outline, uses ASCII corners when requested, and gives
up vertical padding before hiding the selected conversation on short terminals.
Pointer targets follow the painted rows; the border and padding are inert, and
an outside click dismisses the card without activating the page beneath it.
At black and white terminal backgrounds the card moves away from the color
limit and derives local hover and selection steps, including distinct ANSI256
indices. This changes only the floating card; the conversation's palette stays
unchanged.
