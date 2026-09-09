---
kind: added
title: long-running compact steps show quiet elapsed time
pr: 653
surface: [chat, docs]
invalidates:
  - "Compact tool steps only signaled activity with shimmer. After 10 seconds the current step also shows dim elapsed time from its own batch start, with no countdown or icon animation."
  - "There was no elapsed-time suffix on active captions. It now uses spare space after the current caption's last line; it never reflows the description or adds a row. Completed steps and unknown start times have no live timer."
---

The existing frame clock drives whole-second elapsed text. Renaming a caption
preserves its age; a new batch starts again. Icons remain semantic and still,
while the active step's words retain their shimmer. Completion removes the tool
clock; unknown or future start times show no age. The suffix measures elapsed
time, never predicted completion.

Between tools, the latest caption stays still. A separate dot carries activity,
and a known response wait gains awaiting response plus its own elapsed time
after the same ten-second threshold. The response clock starts with the request,
not the preceding tool. Both suffixes use spare cells without reflow; if a waiting
dot cannot fit beside the caption, it uses the existing icon gutter. Detailed
retry and connection information remains available as described in
653-inline-wait.md and 653-connection-recovery.md.
