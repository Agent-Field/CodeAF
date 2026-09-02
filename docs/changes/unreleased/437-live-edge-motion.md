---
kind: changed
title: a lumped stream writes in, and the status-line figures count up
pr: 437
surface: [chat]
invalidates:
  - "A coalesced burst of the reply — everything that piled up while the last frame was built, or a paragraph an endpoint sent at once — landed on the page as one block. The bytes are still kept whole; the live edge now walks them in over a couple of tenths of a second, fast at first and finer at the end. A short burst, about a line, still lands whole. A finished turn snaps whatever is left."
  - "The money and token figures on the status line jumped to each new reading. While a turn is running they walk toward it on the same clock; `/cost` and `/status` still print the exact books, and a restore or a switch still lands on the conversation's own bill at once."
  - "The thinking window's `N tok` and an `ask here` reply on home popped with the same lumps. They walk the same edge. A screen-reader session never paces a byte or a figure."
---

The stream was already honest. What it was not was a stream: a fold, or a
lumpy endpoint, dumped a paragraph and the eye read a stall. The edge is
what moves now, on the clock that was already turning.
