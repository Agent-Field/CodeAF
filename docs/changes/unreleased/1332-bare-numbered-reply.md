---
kind: fixed
title: A list item with no body keeps its marker instead of vanishing
pr: 1332
surface: [chat]
invalidates:
  - "internal/tui2/prose dropped any list item that rendered to zero rows, and its marker with it. An answer of only \"32.\" is one ordered list with one empty item, so it drew nothing at all; it now draws the number, and an empty item inside a longer list keeps its place."
---

`renderer.list` popped each item's marker column with `len(r.out) > before`, so
an item whose body produced no rows left the marker pending and unspent. The
whole reply disappeared for `32.` and `1024.` (#1072), and `1. one / 2. / 3.
three` silently rendered as two items numbered 1 and 3.
