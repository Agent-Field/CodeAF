---
kind: fixed
title: Reply suggestions no longer hold user sends on the remote call lane
pr: 1899
surface: [engine]
invalidates:
  - "Was true: a remote reply suggestion used the ordered conversation lane and a user send could wait behind its model call. Is true now: suggestions run as independent work, like side asks; sends can open their turn while a suggestion is still pending."
---

Suggestion deadlines, generation, cancellation, metering and returned drafts are
unchanged. A held-suggestion wire regression verifies both the immediate send and
the original suggestion response after release.
