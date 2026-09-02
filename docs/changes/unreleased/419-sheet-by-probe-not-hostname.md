---
kind: fixed
title: a base is asked whether it publishes a lane sheet, never read for its hostname
pr: 419
surface: [engine, chat, resident]
invalidates:
  - "The endpoints page was fetched only when the base URL contained `openrouter.ai` (`provider.LaneSheetAvailable`), and both beat seams refused on the same test. Every non-empty base is wired now: the first sheet fetch is the probe, and the base's own answer decides whether it has lanes."
  - "`provider.LaneSheetAvailable` no longer exists. `provider.LaneSheetCertain(base)` is the hostname test that survives, and it is a hint that skips the asking for the shipped router — never a refusal, and false for every other base."
  - "A 404 from the endpoints page was an ordinary error the sheet forgot at once. It is the one answer that makes a base sheetless (`lanes.ErrNoSheetHere`): such a base is not asked again until `sheetTTL` (five minutes) has passed. A 500, a 429 or a timeout leaves the base unasked and it is fetched again on the next beat."
  - "A lane e2e against a loopback stub had to smuggle `openrouter.ai` into the URL's userinfo (`routerBase` in cmd/aforge's lanebeat_test.go) to get a sheet. A stub at its plain `http://127.0.0.1:PORT/api/v1` gets one from its own answer; the trick is deleted, and `lanestub.Server.Sheetless()` stages a base with no endpoints page."
  - "The lane sheet was wired only from `provider.NewClient`, so on `run` and on a saved program the process beat started over an unwired sheet and fetched nothing for a whole interval. `cmd/aforge`'s `startLaneBeat` wires it from the settings through the exported `provider.WireLaneSheet(base, key)` before the beat starts."
  - "`lanes.WireSheet` took three arguments. It takes a fourth, `known bool`: true means the base is known to serve and is never probed; false means ask it."
  - "The manual said nothing about lanes behind a custom base. `lanes.md` now says a proxy, a mirror or a self-hosted router reached through `AFORGE_BASE_URL` has lanes if it answers the endpoints page, and is asked again once every five minutes if it answered 404."
---

A router is recognisable by what it answers and never by a substring of where it
lives. The probe is not a second request: it is the reading of the fetch the beat
was going to make anyway, so the shipped router pays nothing for the law, and a
base that said there is no page costs one quiet request every five minutes rather
than a feature that is silently absent (#373).
