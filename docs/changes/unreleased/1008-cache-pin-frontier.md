---
kind: fixed
title: the cache pin is admitted into the lane chooser's enforced set rather than dropped by it
pr: 1008
surface: [chat]
invalidates:
  - "A cache pin the lane chooser's speed frontier did not name was dropped from the wire by `applyLaneChoice`, so a warm-but-second-fastest endpoint lost the call and one measured chat session rotated through five endpoints in eight calls, each hop a cold 80-110k-token prefix. The pin is now added to the enforced `only` set and still leads its order; only a role's patience refusal or a person's own lane pin removes it."
---

The advisory `provider.order` first-entry was honoured 29% of the time over
ten days of call log, while a strict preference's named machine served 93%.
The pin now rides inside the enforced set — and the two removals it may still
suffer are the one's measured: a role's patience refusal (2026-09-11) and a
person's own lane row.
