---
kind: fixed
title: A selection covers every cell it sweeps, inline code included
pr: 296
surface: [chat]
invalidates:
  - "The sweep's highlight was believed to cover what it copied. It did not: a row carrying an inline code span lit only the cells BEFORE the span — four of sixty-four on a real answer — and a row that opened with one lit none at all. The clipboard was correct the whole time; the paint was lying about it."
  - "A ground step was safe to wrap around any drawn text. It was not: text that paints its own background — an inline code span, a chip — cancelled the step for the rest of the row. `palette.background` now rewrites inner background parameters to its own ground, so every caller of `mark`, `selected` and `cursor` may hand it painted text."
  - "An inline code span kept its raised plane inside a selection. It does not: the selection replaces it, the way every terminal's own selection replaces the background it sweeps over. The code keeps its ink; only the plane under it becomes the selection's."
---

The paint is all a person has to tell them what they are about to copy, so a
highlight with holes in it is a copy nobody trusts — and this one had holes
wherever an answer mentioned a path.

The fault was one line of composition: a background code, the text, one reset.
That is true only of text carrying no background of its own, and prose gives
inline code a raised plane that closes with the compound `\x1b[49;39m` — which
switched the selection's ground off for the whole rest of the row. Fixing it on
the shared step rather than in the sweep brings copy mode, every list's pointer
row and `pastechip`'s marked token back with it.

`internal/manual/chat/keys.md` states the ruling under *Selecting text with your
mouse*.
