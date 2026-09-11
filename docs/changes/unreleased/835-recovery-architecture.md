---
kind: fixed
title: a refused race recovers instead of ending the turn, and account exclusions are learned once
pr: 835
surface: [chat, engine, docs]
invalidates:
  - "A race whose every arm died settled on the PRIMARY's error, and a routing refusal the door had handed to the walk was never laddered if the walk declined or its last arm died of something else (the measured 404, 404, 429 race). Exhaustion now climbs the ladder the door deferred, as one more arm, and a race that still ends settles on the most actionable error: an ordinary failure before a spent ladder before a routing refusal."
  - "A router 404 that named no upstream (`0 endpoints … guardrail restrictions and data policy`) reached the session as `the request itself was refused` — class Work, reported, turn ended. It is now typed on the error (`APIError.Routing`, `provider.RoutingRefusal`) and read as Transport, so the turn is asked again; a malformed request is still Work."
  - "An account-policy exclusion (`Paid model training violation (account settings)`, metadata `…-by-account` / `configure_url`) was filed as a thirty-minute, in-memory refusal of one lane for one model. It is now an account-wide exclusion for every model, held for a day, persisted in `~/.aforge/v3/account-exclusions.json` (merged, never overwritten, across processes on one home), and cleared by an answer that machine serves or by pinning it again."
  - "`All providers have been ignored` was the only body read as a list that emptied the set without asking the demanded machine. The account-policy body is now read the same way (`listEmptied`), so the demanded machine is not struck for the model on that evidence."
  - "A strict lane pin on a machine the account excludes was sent, refused and retired one model at a time. It is now retired up front on every model, with the same `… cannot serve this model; routing on auto for this model until you pin again` line."
  - "A price ceiling priced off the model's list — the first-party machine's own tariff — was sent even when that machine was the only one under it and the account excludes it, so every turn paid one 404 before the walk rescued it. The ceiling is now left off when every sheet row under it is ruled out by the serving set, and a no-demand account refusal teaches the excluded machines when the router's `input_endpoint_count` and the sheet agree exactly."
  - "A rescue arm demanding a machine whose pool answered 429 retried that pool with backoff (29 s, 8 calls in the live replay). It now hands the 429 back to the race at once (3.2 s, 3 calls)."
  - "An auxiliary errand cancelled by its caller was journaled as a failed call. A cancelled errand now writes nothing; one cut by an expired deadline is still written down (the carry-ladder test now stages a real deadline instead of a cancel)."
  - "`lanestub` could not stage an account exclusion or a full pool. It now has `Lane.AccountExcluded` (the live router body, metadata and all), `Profile.Paced` / `PacedAfter` (an in-band 429 after heartbeats, or an immediate 429), and honours `allow_fallbacks: false` (`Ask.NoFallbacks`)."
---

Replicated end to end before the fix: the §1 wire (policy 404, policy 404, in-band 429) ended the turn on the router's sentence through the whole client, and passes now with `Retry n/N` lines and an answer on the first attempt. Live: `go test -tags e2e ./internal/provider -run 'TestRealRouter(AccountExclusion|TheMeasuredRace)'` against the owner's account, and the real binary in tmux on a throwaway home.
