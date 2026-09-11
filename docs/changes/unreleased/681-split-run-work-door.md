---
kind: fixed
title: a turn split by a failed first step stops drawing a second work door above the split
pr: 681
surface: [chat]
invalidates:
  - "A run above a split — the reasoning that happened before a kept step — drew its own `▸ Work · ctrl+e` door, so a turn whose FIRST step failed put two doors with the same key on one page and read as the turn running twice. It is now the ordinary `⠿ thought for 1s · ctrl+e` row: there is one working door per turn."
  - "internal/manual/chat/screen.md said a split turn 'draws a door above the split and the working block below it'. What is above the split now stands where it happened — a kept step as its own row, reasoning behind the thought row — and only the working block is a work door."
  - "Every run of a running turn drew a compact block. A run that is neither the frontier ([liveWork.last]) nor holding a step of its own is drawn through render.go's ordinary walk instead. A non-frontier run that DOES hold steps is unchanged."
---

The compact window is the running turn's chip one tense earlier, and a chip is
the thing a reader counts to know how many pieces of work are on the page. A
stepless window had nothing to draw but its own door, which made a settled
reasoning block look like a second turn in flight — the same misreading #666
took the token column off every run but the frontier to end, arriving through
the other door.
