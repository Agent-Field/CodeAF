---
kind: changed
title: the live edge walks every burst, not only the big ones
pr: 440
surface: [chat]
invalidates:
  - "After #437 a coalesced burst of about a line — 48 bytes or fewer — still landed on the page in one frame. That is most of what a live stream actually is, so the page still jumped with the wire. Every unread remainder now walks in. Only a single short word may land on the event itself. A finished turn still snaps; a screen-reader session still never paces."
---

The arrival shape is no longer the drawing shape. Latency or a folded
handful of tokens used to look the same as a dump; they walk the same
edge as a paragraph now.
