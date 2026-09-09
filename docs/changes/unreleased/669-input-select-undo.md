---
kind: added
title: The message box selects text with the pointer, and ctrl+z takes back what you typed
pr: 669
surface: [chat, docs]
invalidates:
  - "Dragging over text used to select and copy in the CONVERSATION only. Every box a person types into — the message box, home's box, and the composer on tasks, standing, memory, spend, search and settings — now answers the same sweep: the run highlights, double-click takes the word, triple-click the line, and the release copies with the same `copied · N chars` word on the status line."
  - "A selection inside a box used not to exist at all, so nothing could act on one. There is one now and it is live: typing replaces it, backspace and delete remove it whole, a paste drops over it, and any caret motion puts it down."
  - "No box on this surface had an undo. `ctrl+z` now takes back what was typed and `ctrl+shift+z` puts it forward again, in every box — a step is a word rather than a keystroke, a paste and each of the kills are steps of their own, the stack is bounded at 64 steps and by the text it holds, and a SENT message is not undoable (`↑` is still where those live)."
  - "`ctrl+z` used to be unbound and reach nothing. It does not suspend aforge — the terminal runs raw — and it is the undo now. `ctrl+shift+z` only reaches a terminal that reports shift on a control byte; off one, the redo arrives as a second undo."
  - "`shift` with a motion key, and `cmd+a`, used to do nothing in the message box. They select there now. On the seven PLACES `shift+←→↑↓` are still the time window that place is showing, so selection there is the pointer's."
---

A press on a box put the caret under the pointer and returned, so the sweep that
followed reached nothing: the drag machinery parks on the body, and a press a box
has already taken never parks. Mouse reporting takes the terminal's own
drag-select away, and the boxes were the one place left out of the answer the
transcript already gave.
