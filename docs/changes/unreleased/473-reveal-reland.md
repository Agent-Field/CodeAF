---
kind: changed
title: the surface paces the wire's lumps, never its own fold
pr: 473
surface: [chat]
invalidates:
  - "#452 said the walking live edge was off dev and that a burst lands as one block again. It is back. What returns is not #440's rule: a single wire event past 48 bytes is a lump the endpoint buffered, and only that opens a walk. A fold — the run of short deltas `waitEvent` joins while a frame is being built — is drawn whole, however long the run, because the fold is this side of the channel and not the wire's shape. #440 paced every burst, which slowed a token-by-token stream down to inventing latency the connection never had, and left twelve internal/tui3 tests red."
  - "The walk was counted in frames painted. It is elapsed time over the frame interval, read through the surface's clock seam, so a link that paints ten times a second and a machine that paints thirty finish a lump in the same quarter second, and a frame that fires early moves nothing."
  - "Settling a block and letting go of a live pointer were six hand-written pairs of field writes across six files. They are one door, `internal/tui3/livestate.go`, and a go/ast test refuses any site outside it. A block that left the live state without snapping froze mid-word for the rest of the session, because the frame clock only walks the block a live pointer names: `roomAppend` was doing exactly that on a node's page."
  - "The ease-out multiplied its per-slot catch fraction by the number of slots a paint was wide. A paint is one slot wide only when the terminal is keeping up; on a loaded machine it is routinely three, where the linear form asks for 135% of the gap — an overshoot every caller reads as \"land now\". Every walk on this surface therefore collapsed into a single frame on exactly the machines that needed it, while the tests passed because a test advances one slot at a time. The slots compound now."
  - "The live-edge cursor on `entry` and `exchangeRow` was called `shown`. It is `edge`; `shown` is what half the page structs in that package call their own row counts, and the law needed a name meaning one thing."
  - "#437 said the status-line figures count up. They did not: the eased token total was read by no renderer at all, and `app.take` pinned the drawn context weight to the reading it was supposed to be easing towards, so two of the three figures could not move and a real run showed no intermediate value. The token total the task column's foot draws now reads the eased figure, and the weight arms its walk where the weight actually changes (`measureContext`) rather than where the usage lands. This PR is where the figures first count up, proven under a pinned clock by `TestTheStatusLineFiguresCountUp`. The snap rule is unchanged: the exact books the moment the turn is no longer running."
---

#437 had the distinction and #440 dropped it. A model writing to you a few
characters at a time is already writing, and the page says so as it lands. What
pops is the paragraph a stalled wire dumps in one piece, and that is the only
thing worth walking.
