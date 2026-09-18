---
kind: added
title: A Cloudflare Worker relay that folds installs' rows into a signed, versioned pool index
pr: 1093
surface: [engine]
invalidates: []
---

The relay under `relay/` accepts NDJSON measurement rows at `POST /pool/v1/rows`,
keyed by the install's `X-Codeaf-Install` header, folds each install's rows into
per-install per-day running totals in KV, and once an hour publishes a signed,
versioned JSON index at `GET /pool/index.json` beside its Ed25519 detached
signature at `GET /pool/index.json.sig`. The document is what
`internal/pool/index` reads and is signed with a key whose public half is built
into the client.
