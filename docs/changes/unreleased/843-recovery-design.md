---
kind: internal
title: the failed-call family has one design, and a census of what the call log actually says
pr: 843
surface: [docs]
invalidates:
  - "The 429 / account-404 / stall family was believed closed by #793, #794, #786 and #835. Each closed the instance it was opened for; the shape — eleven controllers on one request path, each owning its own retry budget, a retry meaning the same bytes to the same machine, a ledger keyed on the lane asked for rather than the machine that served — is still there and is what `docs/design/recovery/DESIGN.md` now names. Waves R0–R4 in its §7 are the work; R3 (one dispatcher) lands after R0, R1, R2 and R4."
  - "It was believed that most failed calls are the provider's fault. The ten-day census beside the design (`census-20260910.md`) says 57% of recorded failures are the client cancelling itself — 60 s / 90 s duration walls that cut 780 streams which had already produced a first token, hedge losers, deadlines — and that 53% of retry chains never left the lane they started on."
---

Owner-approved 2026-09-10 after a task hung three minutes on `waiting · rate limited`
while nine identical requests went to one rate-limited machine.
