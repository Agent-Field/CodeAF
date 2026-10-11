---
kind: changed
title: The factory serves no web dashboard; /v1 is its only face
pr: 1800
surface: [engine, docs]
---

The dashboard moves to the codeaf app. `GET /` on the factory answers one plain
line pointing at the TUI and the app; every `/v1` endpoint, the event stream,
the access token and the session cookie are unchanged. The browser smokes and
`make web` are gone; approve to PR is covered by an API-level Go test.
