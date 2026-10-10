---
kind: changed
title: Desktop All places follows the root Home design
surface: [chat]
invalidates:
  - "Root Home used a 220px search field and three columns with a Places label; Places 8c now supplies a 240px field and four top-level tile columns without the extra label."
  - "Root search hid all unplaced chats; it now filters their titles while matching place names and ancestor paths."
---

The root-specific component keeps real engine totals and unplaced chat rows.
Its tile heights follow the measured Places 8c bounds, and narrow panes reduce
the column count without hiding search or introducing horizontal overflow.
