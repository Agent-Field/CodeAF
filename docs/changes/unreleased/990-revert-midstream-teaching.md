---
kind: removed
title: The mid-stream ledger teaching of #964 is reverted until one stream teaches once
pr: 990
surface: [engine]
invalidates:
  - "A running stream taught the lane ledger a partial sighting at most once a minute, with the settlement sighting rebased to cover only the tail (#964). It is reverted: a stream teaches the ledger once, at settlement, exactly as before #964, and #891 §1 (a call teaches nothing until it settles, so a retry can pick the machine the last call was still throttled by) is open again."
  - "#964's change entry said one stream is one measurement told in chapters, never the same span twice. On the winning arm — every ordinary call — that was false: the rebase lived only on the losers' `streamWatch.sighting`, while the winner settled through `noteVelocity → noteLane` and taught the whole stream on top of its chapters."
---

#964 merged 2026-09-12 03:11Z with CI three-green and no architecture review. The
post-merge review against the speed wave's bar found the headline invariant false on the
dominant path (double teaching, the defect hedge.go documents as fixed for racing arms),
the first-token wait filed N+1 times, a one-minute cadence with no derivation, a chapter of
sixty seconds teaching as hard as a whole stream, three constructors for one Sighting, and
a synchronous flock+append on the per-delta read loop. Nothing was measured against L10's
replay (#922), which had already shown the chooser switching about seven times more than
its advantage justifies.

This is a clean `git revert` of 75af6ee57. #964's idea returns when the settlement rebase
lives at ONE door shared by winner and loser, partials carry no TTFT, the cadence and the
window-scaled noise are derived, `Note` is off the read loop, a non-overlap law test
guards it, and `make replay` on the Spark shows switches and regret do not rise. That
re-landing carries its own entry.
