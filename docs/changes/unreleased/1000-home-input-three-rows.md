---
kind: changed
title: Home's box is three rows tall, typed in or not, and one command clears every engine
pr: 1000
surface: [chat, engine, docs]
invalidates:
  - "Home's foot box grew to fit its draft and no further, so the commonest state — a question one sentence long — drew a SINGLE row between the rule above it and the hint below it. The owner's reading was that it is too thin to notice and does not look like somewhere to type, and the cause is that one row of text bounded by two rows of chrome has no mass of its own. It now stands three rows high from the first character (`homeDraftFloor`), padded below the draft so the first line typed stays on the first row."
  - "The box no longer changes height while somebody types. The floor and the ceiling are both three, so a sentence that wraps to a second row does not grow the box and does not step the list above it down a row. Anything that remembers home's box as growing with its content is wrong; past three rows the window scrolls under the caret exactly as it did."
  - "An empty box is three rows as well, not one. The place's dim sentence is on the first row and the other two are held open under it, so the one thing on the screen a person types into is a block they can see BEFORE they have typed anything — somebody who cannot find the box has nothing to type into it. It also means the foot does not move on the first keystroke, where a box that jumped from one row to three would shift the list up under the hand reaching for it. The resting rows are the box's silhouette and not its surface: the press span stays empty, so a click on them falls through to the place underneath exactly as a click on the resting row always has."
  - "The floor is spent out of rows the frame has OVER an eighty by twenty-four terminal, never out of that frame's own body — so it appears at twenty-six rows and taller and below that the box is the single row it has always been. Held open unconditionally it cost three rows everywhere, and at twenty-four those three are not spare: home dropped a panel off the bottom of its column, an empty place drew its rule where its whisper had been, and the rail's standing section was squeezed out by a long roster. Anything that remembers this as a height the box is always three rows at is wrong."
  - "The composer's floor is the CONVERSATION'S floor too, not home's alone. The chat's foot and a place's foot are the same rows, and `esc` between them may not move any of them — a law that was broken and fixed once already. `boxFloor` is asked by the chat's `inputBlock`, which its height and its drawing both go through, and by every place's frame. On a frame tall enough for it the conversation's transcript is two rows shorter than it was."
---

`aforge engine --stop-all` is here too, because it is the other half of the same
afternoon: the stale-engine refusal names one workspace, and a person who does
not know WHICH of their folders is the problem was left reading a directory of
hashes. `enginehost.Held` reads the plain-text workspace each host writes beside
its socket, and the sweep stands every one of them down, naming each as it goes.
A directory whose host has already gone is counted rather than printed, so a
sweep does not bury the one line that matters under thirteen saying nothing was
there.

The caret arithmetic in `pages.go` is untouched. It derives the caret's row by
subtracting the block's height from the rows placed, so padding at the bottom
moves both by the same amount — verified by driving the real binary against the
demo home, where the caret lands on the last character of a two-row draft.
