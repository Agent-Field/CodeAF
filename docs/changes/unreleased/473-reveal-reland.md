---
kind: changed
title: the surface paces the wire's lumps, never its own fold
pr: 473
surface: [chat]
invalidates:
  - "#452 said the walking live edge was off dev and that a burst lands as one block again. It is back. What returns is not #440's rule: a single wire event past 48 bytes is a lump the endpoint buffered, and only that opens a walk. A fold — the run of short deltas `waitEvent` joins while a frame is being built — is drawn whole, however long the run, because the fold is this side of the channel and not the wire's shape. #440 paced every burst, which slowed a token-by-token stream down to inventing latency the connection never had, and left twelve internal/tui3 tests red."
  - "The walk was counted in frames painted. It is elapsed time over the frame interval, read through the surface's clock seam, so a link that paints ten times a second and a machine that paints thirty finish a lump in the same quarter second, and a frame that fires early moves nothing."
  - "Settling a block and letting go of a live pointer were six hand-written pairs of field writes across six files. They are one door, `internal/tui3/livestate.go`, and a go/ast test refuses any site outside it. A block that left the live state without snapping froze mid-word for the rest of the session, because the frame clock only walks the block a live pointer names: `roomAppend` was doing exactly that on a node's page."
  - "The live-edge cursor on `entry` and `exchangeRow` was called `shown`. It is `edge`; `shown` is what half the page structs in that package call their own row counts, and the law needed a name meaning one thing."
  - "The status line's money and context figures are still not observed to ease in a real run, and `app.shownTokens` is eased but read by no renderer. #437's changelog claimed both walk. Only the text half is proven; the meter half is tracked separately and this PR does not fix it."
---

#437 had the distinction and #440 dropped it. A model writing to you a few
characters at a time is already writing, and the page says so as it lands. What
pops is the paragraph a stalled wire dumps in one piece, and that is the only
thing worth walking.
