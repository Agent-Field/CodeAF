---
kind: changed
title: one call asks for one set of machines, whichever door its request left by
pr: 934
surface: [engine, chat, docs]
invalidates:
  - "`withLaneChoice` stamped a lane choice only on the streamed door, and every OTHER call drew a fresh one inside the encoder — a contract `laneChoiceFor`'s own doc comment stated (\"a non-streamed call has no watch to agree with, so it decides here\"). Since the choice is a sampled draw seeded with the moment it is asked at, and one call encodes its request once per attempt, once per ladder rung and once more for the object the refusal door re-derives, one call could ask for three different sets of machines. There is one draw now, `Client.drawLaneChoice`, called only by `Client.withLaneChoice`, and the encoder reads `knobs.laneChoice` or applies nothing."
  - "`requestSet(knobs)` read empty on any call the streamed door had not stamped, so `control.Plan.Lane` and `Plan.Alts` were an OPEN set and the move generator walked machines the wire had never named. Every call that reaches the wire now carries its one choice: `Client.sendShaped` — the one door every send passes through, where the call's one budget was already stamped — stamps it for every road that is not the streamed one, `Client.StreamComplete` included."
  - "A belief that arrived while a call was in flight — the lane beat's own refresh landing a second late, a refusal written into the ledger between two attempts — changed the machines the REST of that call asked for. It no longer does: the ranking is decided before the first byte leaves and lasts the whole request, and what still changes between two sends of one call is only the machines that have refused THAT call. The new belief is spent on the next request."
  - "`wirePreferences`, `providerPreferences`, `applyLaneChoice` and `onlyLane` each took the `*ai.Request`, and took it only so the encoder could decide the choice again. They no longer take it."
  - "`onlyLane`'s doc said re-deriving the preference object was safe for `only` alone, because an unstreamed call's ranking could differ from the wire's. It is exact for every field now — the object this composes is the object the body carried."
---

The seam PR #925 took its demand back out over (its report, *Deliberately not in
this PR — issue #891 §3*): the chooser cannot DEMAND a set until the plan, the
watch and the body are looking at the same one. No routing policy moves here —
same frontier, same ranking, same vetoes, asked once instead of once per encode.

Replicated on the wire first, in `internal/provider/onechoice_test.go` against
`lanestub`, with the belief arriving mid-call: on `dev` both doors send body 1
with no order and body 2 with `order=[quicksilver]`. `TestOnlyOneFunctionDrawsACallsLane`
is the law, on the `laws` gate, and its allowlist is data with a reason on each
line.
