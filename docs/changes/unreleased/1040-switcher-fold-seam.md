---
kind: fixed
title: The switcher's fold says where the tabs stop, and every refusing row says why
pr: 1040
surface: [chat, docs]
invalidates:
  - "With `→ show closed` open, the card drew one undifferentiated list — nothing said which rows were tabs and which were not, so a conversation whose tab you had just closed looked identical to one that is open (same `○`, same live clause, because it IS still held and running). One dim `hopClosedLabel` (`closed`) is now drawn at the seam. It is not a row: it takes no `hopSpot`, the cursor skips it, and it is not drawn while the list is only tabs."
  - "`hopRest` asked the disk for the project folder to decide the row's GLYPH but left `switcherRow.gone` false, so `hopRestNote` never said `that folder is gone` — the row wore the refusing `✕` with an empty clause. The answer is taken once and both readings use it. Every row drawn with `✕` now carries its reason: `open in another window` or `that folder is gone`."
  - "`internal/manual/chat/keys.md` did not say what the card's `✕` means. It does now, in both the layout section and the row-clause table: `✕` is the card refusing to open that row, and it is NOT the tab-close mark a person presses. The two shapes are near-identical and mean different things one line apart."
---

The owner's report: "tabs that I close don't end up with an x icon when show
closed is enabled. Instead they look just like the open tabs. What's the
difference between these tabs and those that do have an x icon?"

Both halves of that were the surface's fault. There was no difference drawn
between a tab and a closed tab once the fold was open, and the `✕` they had
reasonably read as "closed" is `tokens.GlyphFailed`, which on this card means the
row refuses — and on one of its two cases said nothing at all about why.

The seam is a word rather than a rule, because the house rule is that nothing is
outlined and this surface separates things with ink. It is the fold's half of the
`open` the head already says.

Not changed: the `✕` itself. That a person presses `✕` to close a tab and reads
`✕` as a refusal one line below is a real collision in the vocabulary, but the
mark is `tokens.GlyphFailed` through the one door every surface uses, and moving
it is a change to `docs/design/icons/DESIGN.md` and the slot table rather than to
this card.
