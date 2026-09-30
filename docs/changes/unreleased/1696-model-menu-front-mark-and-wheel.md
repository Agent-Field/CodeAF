---
kind: fixed
title: the /model menu's current model wears no band and the picker owns the wheel
pr: 1696
surface: [chat]
invalidates:
  - "The /model list drew the model in use on the ladder's selected ground, a full-width highlighted row wherever the cursor was. The current model keeps its accent and its weight and wears no band now; the cursor alone indicates the highlighted row."
  - "A wheel notch while the /model picker was up fell through to the transcript and scrolled the conversation behind the modal. The picker claims the wheel now and walks its own list, clamped at both ends like the keys."
---

The grammar is the picker's own (`palette.frontUnlifted`), so every other list
that draws through the shared overlay renderer keeps the ladder's selected step
for its front mark.