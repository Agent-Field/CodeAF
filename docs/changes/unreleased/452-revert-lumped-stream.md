---
kind: internal
title: the walking live edge (#437, #440) is off dev again until its tests are green
pr: 452
surface: [chat]
invalidates:
  - "#437 and #440 said a coalesced burst of the reply walks onto the page over a couple of tenths of a second, that the status-line money and token figures walk toward each new reading, and that every unread remainder walks in. None of that is on dev: both were reverted together because they left twelve internal/tui3 tests red (a streamed reply cut at `som`, `the reply never arrived`), bisected to 5995a6e7 and reproduced on a clean tip. A burst lands as one block again, exactly as before #437, and the figures jump. The two re-land with the fix under their own numbers; this entry goes with them when they do."
---
