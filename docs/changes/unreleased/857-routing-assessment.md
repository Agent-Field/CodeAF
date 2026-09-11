---
kind: internal
title: the router's fast lanes were unreachable and the assessment says why
pr: 857
surface: [docs]
invalidates:
  - "`lane.talk: auto` was believed to be OpenRouter's own routing. It is not: the belief writes `provider.order`, and only `lane.talk: openrouter` hands the choice to OpenRouter."
  - "The chooser was believed to reach the lanes it ranked first. On the wire `max_price` (list × 1.25) filters every lane dearer than the cheapest before `order` is read, so for deepseek-v4.1-flash the two fastest lanes are reachable only through a rescue arm."
  - "A task node was believed to be routed for speed like a turn. It is routed at λ = 0: price only, with the frontier pruned to 1.25× the cheapest."
---

No routing code changes. `docs/design/routing/ASSESSMENT-20260911.md` carries three
days of the call log, a replay of it through the real chooser
(`internal/lane/replay_bench_test.go`, build tag `replay`), a recorded request body
and a live per-lane probe, and names the six policy changes that would let the
belief core act on what it already knows.
