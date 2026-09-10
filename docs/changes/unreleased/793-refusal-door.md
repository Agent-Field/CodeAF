---
kind: fixed
title: a 429 inside an open stream now paces the pool that served it, like one that arrives as a status
pr: 793
surface: [engine]
invalidates:
  - "retry.go said a 429 is \"answered by the wait the provider itself named\" and refusalobject.go returned nothing at all for one. That reading held only for a 429 arriving as an HTTP status: the pacing note ran off `response.StatusCode` in the retry loop, so a 429 delivered INSIDE an already-open 200 stream reached no ledger — not the pace, not the strike — and the next encode carried the same preferences straight back to the pool that had just refused. A 429 that names a pool is now a class of the refusal object (`refusalPaced`) and paces that lane, whichever transport carried it."
  - "The pace and the strike were two writers of one ledger, in two files, reached by two paths. There is now ONE door, `Client.refuseLane` (velocity.go): every refusal is paced, struck, or ignored there, and `internal/provider/refusaldoor_law_test.go` reads the package with go/ast and fails the build if a second writer appears. `Client.strikeRefusal` is gone by that name; `Client.refuseUpstream` keeps its name and takes the served lane and the provider's named wait."
  - "`pacedProviderName` was a second decoder of `error.metadata.provider_name`, kept for the retry loop. It is deleted: `apiError` already reads that field, for every status and both transports, and two readers of one sentence is how the two paths came to know different things about the same refusal."
  - "A refusal delivered mid-stream reached `RefusalFrom` — and through it internal/taxonomy's Evidence — with an EMPTY provider unless one call site happened to fill it in, because the router omits `provider_name` when the stream already named its provider in the chunks. The door now stamps the serving name onto an unnamed refusal before anything reads it (`nameServed`), so an in-stream refusal and an HTTP one carry the same status and the same upstream everywhere downstream."
  - "The manual said nothing about rate limits at all. `internal/manual/chat/lanes.md` now has \"When a machine is too busy\": a named pool is stepped around for the wait it asked for (five minutes when it named none), it counts wherever the message arrived, and a rate limit naming nobody is the account's own ceiling and is waited out rather than sent to a second machine."
---

Measured on 2026-09-10, chat conversation `57d51779f63ac603`: a turn on
`deepseek/deepseek-v4.1-flash` died after one 502 from DeepInfra and three 429s
served by Io Net at 13:00:39, 13:01:04 and 13:01:32. All three arrived inside an
open HTTP 200 — the journal's rows say status 200 and `API error (429): Provider
returned error (via Io Net)` — so nothing was ever written about Io Net and the
router served it three times in ninety-three seconds. An account-wide 429, which
names no pool, is unchanged: it implicates no machine and is waited out rather
than multiplied across machines.
