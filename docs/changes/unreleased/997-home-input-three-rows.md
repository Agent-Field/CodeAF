---
kind: changed
title: Home's box is three rows tall the moment you type in it
pr: 997
surface: [chat]
invalidates:
  - "Home's foot box grew to fit its draft and no further, so the commonest state — a question one sentence long — drew a SINGLE row between the rule above it and the hint below it. The owner's reading was that it is too thin to notice and does not look like somewhere to type, and the cause is that one row of text bounded by two rows of chrome has no mass of its own. It now stands three rows high from the first character (`homeDraftFloor`), padded below the draft so the first line typed stays on the first row."
  - "The box no longer changes height while somebody types. The floor and the ceiling are both three, so a sentence that wraps to a second row does not grow the box and does not step the list above it down a row. Anything that remembers home's box as growing with its content is wrong; past three rows the window scrolls under the caret exactly as it did."
  - "An empty box is still ONE row, drawing the place's own dim sentence. That is unchanged and deliberate: this screen is a list first, and three blank rows held open for a box nobody is typing in would be the cockpit it is not."
---

The caret arithmetic in `pages.go` is untouched. It derives the caret's row by
subtracting the block's height from the rows placed, so padding at the bottom
moves both by the same amount — verified by driving the real binary against the
demo home, where the caret lands on the last character of a two-row draft.
