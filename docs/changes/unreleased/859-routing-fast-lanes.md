---
kind: fixed
title: the router can reach the fast lanes again, and a refusal now counts against a lane
pr: 859
surface: [engine, chat]
invalidates:
  - "A request the belief had ordered still carried `max_price` (list × 1.25). It no longer does: the frontier priced those lanes with the person's λ, and the wire ceiling rides only the sort-word path where no belief has spoken."
  - "A task node was routed at λ = 0 — price only, frontier pruned to 1.25× the cheapest. λ now has a floor of a quarter of a person's attention (`lane.UnattendedValue`); only the routing row's `price` word means zero."
  - "A 429 or a struck lane reached the strike ledger and never the belief. It is now an `Outcome{Refused: true}` on a new availability axis (`Belief.Availability`, five-minute half-life) that divides the expected wait; quality is not charged for it."
  - "The quality gate read the Beta's upper bound whatever the evidence. It reads the mean once a belief carries twelve observations."
  - "No lane was ever too slow in absolute terms. A service floor — 6 s first token, 30 tok/s, half of requests answered, on a belief the ledger is sure about — refuses it, and never empties the set."
---

Three days of the call log put the router's mean regret on deepseek-v4.1-flash at
31.9 s against 1.2 s with these five changes replayed
(`docs/design/routing/ASSESSMENT-20260911.md`; `go test -tags replay ./internal/lane/`
is the bench). Censored timing for cut streams is still owed.
