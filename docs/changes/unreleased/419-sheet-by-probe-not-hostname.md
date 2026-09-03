---
kind: fixed
title: a base is asked whether it publishes a lane sheet, never read for its hostname
pr: 419
surface: [engine, chat, resident]
invalidates:
  - "The endpoints page was fetched only when the base URL contained `openrouter.ai` (`provider.LaneSheetAvailable`), and both beat seams refused on the same test. Every non-empty base is wired now: the first sheet fetch is the probe, and the base's own answer decides whether it has lanes."
  - "`provider.LaneSheetAvailable` no longer exists. `provider.LaneSheetCertain(base)` is the hostname test that survives, and it is a hint that skips the asking for the shipped router — never a refusal, and false for every other base."
  - "A 404 from the endpoints page was an ordinary error the sheet forgot at once. It is the one answer that makes a base sheetless (`lanes.ErrNoSheetHere`): such a base is not asked again until `sheetTTL` (five minutes) has passed. A 500, a 429 or a timeout leaves the base unasked and it is fetched again on the next beat."
  - "A 404 is not that answer by its status; it is by its body. The live router answers 404 twice over: `{\"error\":{\"message\":\"Not Found\",\"code\":404}}` for a model it does not publish, and an HTML page for a route it does not serve. Only a 404 whose body is NOT the router's error envelope (`provider.sheetNotFound`, decoded through the same `apiError` every refusal is) wraps `lanes.ErrNoSheetHere` and makes the base sheetless; the envelope 404 is a quiet per-model error (`errNoSheetForModel`) that marks nothing about the base, so a first beat model the router does not publish no longer holds a proxy or mirror sheetless for five minutes. `lanestub.Server.Sheetless()` answers the HTML body now, and its unknown-model 404 stays the envelope."
  - "A lane e2e against a loopback stub had to smuggle `openrouter.ai` into the URL's userinfo (`routerBase` in cmd/aforge's lanebeat_test.go) to get a sheet. A stub at its plain `http://127.0.0.1:PORT/api/v1` gets one from its own answer; the trick is deleted, and `lanestub.Server.Sheetless()` stages a base with no endpoints page."
  - "The lane sheet was wired only from `provider.NewClient`, so on `run` and on a saved program the process beat started over an unwired sheet and fetched nothing for a whole interval. `cmd/aforge`'s `startLaneBeat` wires it from the settings through the exported `provider.WireLaneSheet(base, key)` before the beat starts."
  - "`lanes.WireSheet` took three arguments. It takes a fourth, `known bool`: true means the base is known to serve and is never probed; false means ask it."
  - "The manual said nothing about lanes behind a custom base. `lanes.md` now says a proxy, a mirror or a self-hosted router reached through `AFORGE_BASE_URL` has lanes if it answers the endpoints page; a base with no such page at all is remembered as having none for five minutes, and a router that merely does not publish one model is not."
---

A router is recognisable by what it answers and never by a substring of where it
lives. The probe is not a second request: it is the reading of the fetch the beat
was going to make anyway, so the shipped router pays nothing for the law, and a
base that said there is no page costs one quiet request every five minutes rather
than a feature that is silently absent (#373).
