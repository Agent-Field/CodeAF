---
kind: fixed
title: a reply that is only a number and a full stop draws instead of vanishing
pr: 1746
surface: [chat]
invalidates:
  - "A model reply whose whole text was a number and a full stop (`32.`, `1024.`) parsed as a Markdown ordered list with one empty item, so the list renderer drew the marker column, found no body, and discarded the marker too. The reply rendered to zero rows and the deck skipped it: the fold showed only the thought row though the transcript held the text. An ordered-list item with no body now draws its marker as the literal text the reader sent, so `32.` stands under the chip; `1. one` and multi-item lists still draw as lists."
---

Found while running the number-answer journey against `dev`: asking the chat for
"only the number followed by a period" produced `32.`, which the surface dropped.
`renderer.list` in `internal/tui2/prose` pushed the marker, rendered the empty
item, and popped it as undrawn, taking the marker with it. The fix draws the
marker when the item body produced nothing, which is the only case a bare
`<number>.` reaches.
