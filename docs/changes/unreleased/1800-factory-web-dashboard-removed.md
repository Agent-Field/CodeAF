---
kind: changed
title: The factory serves no web dashboard; /v1 is its only face
pr: 1800
surface: [engine, docs]
invalidates:
  - "The binary served dashboard pages for jobs, settings and browser approvals. The team view now lives in the codeaf app (Agent-Field/codeaf-desk); factory GET / is a one-line pointer to the clients and /v1."
  - "Dashboard builds and browser smokes ran in this repository. They now belong to the app repository; the factory's publish acceptance uses the Go API test."
---

The dashboard moves to the codeaf app. `GET /` on the factory answers one plain
line pointing at the TUI and the app; every `/v1` endpoint, the event stream,
the access token and the session cookie are unchanged. The browser smokes and
`make web` are gone; approve to PR is covered by an API-level Go test.
