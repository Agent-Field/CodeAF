---
kind: fixed
title: a ground step owns every cell it covers — a sweep lights code spans and chips
pr: 303
issue: 294
surface: [chat]
invalidates:
  - "That the sweep's highlight already covered what it copied. It did not: a row carrying an inline code span lit only the cells before the span — 4 of 64 on the row the field failure was seen on — and a row that opened with one lit nothing at all, while the clipboard took the whole row. The paint lied about the copy, and now it cannot."
---

`palette.background` was one background code, the text, one reset. That is true
only of text carrying no background of its own. An inline code span sets its own
ground (`48;2;…` on the raised plane) and closes with the compound `49;39`, so
the span's ground took the plane off the step, and the compound clear switched
the step's ground off for the rest of the row.

`holdGround` walks the wrapped text and re-lays the step's ground under every
cell: an inner background set becomes the step's ground, an inner background
clear becomes the step's ground, and a full reset keeps its reset and re-lays
the ground behind it. Every form the styler can emit is answered — `48;5;n`,
`48;2;r;g;b`, the sixteen-colour `40`–`47` and `100`–`107`, the bare `49`, the
compound `49;39` prose actually emits, and `0`, which a terminal also spells as
no parameters at all. Inks and attributes are untouched, so a marked code span
keeps its colour, its bold and its italic.

THE SELECTION WINS OVER THE CHIP, and that is the ruling rather than a side
effect. The mark step is the loudest rung of the ground ladder; every terminal's
own selection replaces the background it sweeps over, and this one reads the
same.

One root, three callers healed at once: the sweep's mark (`palette.mark`), copy
mode's selection (`palette.selected`), and the pointer's row (`palette.cursor`).
