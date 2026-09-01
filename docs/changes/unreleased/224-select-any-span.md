---
kind: fixed
title: dragging over text selects characters, not rows — and a double-click takes the word
pr: 224
issue: 222
surface: [chat]
invalidates:
  - "A sweep over the conversation or a room selected and copied WHOLE ROWS: press on
    the third word and release on the fifth and the whole line landed on the clipboard,
    with `copied · N lines`. The selection is a stream now — from the pressed cell to
    the end of its row, every row between in full, the last row up to the pointer —
    and a sweep inside one row is the span between its two columns. What is highlighted
    is what is copied, to the character."
  - "There was no way to take one word. Double-click takes the word under the pointer
    (a path, a hash, a flag, a dotted name are each one word; a closing full stop is
    left behind), triple-click takes the row, and the status line says which:
    `copied · 1 word`, `copied · 1 line`, `copied · 14 chars`, `copied · 3 lines`."
  - "The keys page said the selection was 'by rows — whole lines, not characters'."
---

The row-based sweep was a stated design: a transcript is made of lines, and a row has
no seams around wide glyphs. Both true, and neither is what a hand is asking for — on
every screen a person has used, pressing on one character and releasing on another
means those characters, and a whole line landing instead read as broken rather than
as opinionated. So the selection is a stream, the way a terminal's is, on the same
content-anchored seam the row sweep already had; columns need no conversion because
the body is drawn from column zero and only scrolls vertically.

The wide-glyph seam is answered by snapping: a column is converted through the row's
plain text, so a glyph is wholly in or wholly out and a selection never emits half of
one. A double-click on a tool call or a fold is still two clicks — open, then shut —
because a button acts on every click; the rule that decides which rows are buttons is
the transcript's own click resolver read from the other side.
