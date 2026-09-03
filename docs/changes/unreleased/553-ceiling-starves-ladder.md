---
kind: fixed
title: a price ceiling that empties the endpoint set no longer fails the turn
pr: 553
surface: [chat, engine, docs]
invalidates:
  - "models-and-cost.md promised that when no endpoint can serve a request under the price cap, aforge lifts the cap rather than failing the turn. In a lane race it did not: `sendRecovered` handed every recognised refusal to the walk while an untried lane NAME existed, the walk was refused by the hedge purse, and the raw 404 reached the screen — 17 times in one call ledger, with zero relaxed retries. `canWalk` now answers whether a walk will actually start (a serving lane the purse can fund), and the ladder climbs on the arm the purse will not rescue."
  - "A rescue arm and a strict lane pin sent `provider.only` beside the same `provider.max_price` as the primary. With the cap admitting only the first-party endpoint, every rescue to another machine was refused with `permits only: <lane>` — and since #368 that refusal struck the innocent lane out of the serving set. A lane demand now carries no ceiling: the chooser already priced that machine."
  - "The memo that stops a refused ceiling being sent twice was written only inside the ladder, so a refusal the ladder never saw taught nothing and every later call paid the same 404. It is now written at the refusal itself, whether the race walks or the ladder climbs."
  - "The chat manual said nothing about the router's `guardrail restrictions and data policy` refusal or the `Paid model training violation` line under it. models-and-cost.md now has a section in those words: the cap left one endpoint, the account's OpenRouter privacy setting excludes it, and what aforge does about it."
---

The failure was measured live on 2026-09-02 against deepseek/deepseek-v4-pro-0813: the router's own
`routing_funnel` went 18 endpoints → 1 at "Filter by Max Price" and died at "Filter by Guardrails".
The ladder built for that case (2026-08-28, `ceiling_policy_test.go`) was proved only unraced; the
raced fixture had three lanes, exactly what the default purse funds, so the hole was invisible.
