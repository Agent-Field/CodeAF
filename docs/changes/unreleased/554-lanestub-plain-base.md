---
kind: removed
title: lanestub's router-shaped second address is gone; the plain base is the only base
pr: 554
surface: [engine]
invalidates:
  - "A test that wanted a lanestub to hand out lanes or take a routing preference had to point the build at a second mount whose path was spelled `openrouter.ai` (`Server.RouterURL()`). No longer true: since #419 the sheet comes from what a base answers and since #433 so does whether it carries a preference, so the plain `Server.URL()` gets the whole of the lane behaviour, and the second mount, its path prefix and the accessor no longer exist."
  - "The #266 refused-lane e2e drove the binary at the dressed address. It now drives it at the plain base and passes there."
---

One mount at one address. The two provider tests that still wore the costume stage the
shipped router's hint at the seam it really reaches: `LaneSheetCertain` is pinned for the
shipped hostname, and the same `known` is filed for the plain address through
`lanes.WireSheet`, which is exactly the state a client on the shipped router starts its
first turn in. The hint test names the shipped router by hand and makes no request.
