---
kind: fixed
title: the current model wears no band of its own and every model list owns the wheel
pr: 1696
surface: [chat]
invalidates:
  - "The /model list drew the model in use on the ladder's selected ground, a full-width highlighted row wherever the cursor was. The current model keeps its accent and its weight and wears no band of its own now; the cursor supplies its band, and a pointer hovering another row lifts that row too."
  - "A wheel notch fell through open model lists and moved the page beneath them. /model, settings slots and roles, home's draft list and the task composer's list now claim the wheel and walk three rows a notch, clamped at both ends like the keys; closing the list returns the wheel to the page."
---

The grammar is the picker's own (`palette.frontUnlifted`), so every other list
that draws through the shared overlay renderer keeps the ladder's selected step
for its front mark.