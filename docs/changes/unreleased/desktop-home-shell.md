---
kind: fixed
title: Desktop Home keeps its column and composer aligned at narrow widths
surface: [chat]
invalidates:
  - "Home only used 16px gutters through 600px, and centered the composer slot only for populated places; all Home states now share the column and use the available width through 850px."
---

The Home shell keeps its dock outside the masked section scroller. Browser
contracts cover 320, 600, 850 and 1200px in both appearances, including empty,
Now, root and loading states. The existing shared PlaceHeading is reused.
