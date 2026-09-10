---
kind: fixed
title: The up arrow recalls in every conversation, not only the first one
pr: 747
surface: [chat]
invalidates:
  - "In a conversation opened beside the first — from home's rows, `/new`, or the target on home's rule — the up and down arrows scrolled the transcript instead of walking the last things typed. The fleet handed those conversations no recall store (cmd/aforge's `engineFleet.bundle` left `History` nil). Every conversation on a connection now gets the machine's own `history.jsonl` door, so `↑` on the first line of the box recalls and `↓` walks forward everywhere."
---
