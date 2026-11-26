---
kind: fixed
title: Keep app change cards available until Apply or Discard
pr: 2967
surface: [app]
invalidates:
  - A marked edit used to appear only as an editing checkpoint with Keep.
  - An app assistant could be routed to generic work instead of proposing an app change.
---

Marked edits and granted assistant requests produce a checked change card
that reopens after restart. Apply publishes a new version, and Discard
returns to the current one.
