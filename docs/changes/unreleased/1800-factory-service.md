---
kind: changed
title: The factory manual describes the service and team attachment
pr: 1800
surface: [chat, remote, docs]
invalidates:
  - "The manual described dashboard pages served by the binary. The team view now lives in the codeaf app (Agent-Field/codeaf-desk); GET / on the factory is a one-line pointer to the clients and /v1."
  - "The manual said the factory page draws nothing connected yet over --host or --at. These windows can read the team factory named by codeaf attach on this machine."
  - "The manual described a window handing job actions to a local runner owner. Actions now go through the factory service, and attached workers execute jobs through leases."
---

Document `codeaf factory`, the host config, `serve.json` discovery, access tokens,
and `codeaf attach` / `codeaf detach` as implemented on `factory/page`.
Remove the former dashboard screen descriptions while retaining the service's
access form, JSON API, event stream and record export. Retrieval probes check
that team attachment, the dashboard address, discovery and remote chat questions
reach the sections that answer them.
