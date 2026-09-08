---
kind: added
title: long-running compact steps show quiet elapsed time
pr: 653
surface: [chat, docs]
invalidates:
  - "Compact tool steps only signaled activity with shimmer. After 10 seconds the current step also shows dim elapsed time from its own batch start, with no countdown or icon animation."
  - "The icon wave stated that no timer was added. An elapsed suffix now uses spare space after the current caption's last line; it never reflows the description or adds a row. Completed steps and unknown start times have no live timer."
---

The existing frame clock drives whole-second elapsed text. Renaming a caption
preserves its age; a new batch starts again. Icons remain semantic and still,
while the words retain their shimmer. Generic waiting and retry states keep
their existing timing rather than receiving a duplicate timer.
