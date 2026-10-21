---
kind: fixed
title: model usage accounting covers compatible providers and separates estimates from measurements
pr: 1887
surface: [engine, docs]
invalidates:
  - "Streaming requests previously sent only the router usage opt-in. Connected compatible APIs now receive stream_options.include_usage, with a narrow fallback for endpoints that explicitly reject it on 400 or 422. The default router road keeps its original wire shape; unrelated refusals gain no new repair."
  - "Missing provider counts previously stayed unknown even when text was observed. Missing text components now have separate versioned estimates; reported zeroes, partial counts and total-only receipts remain measurements, and media or unseen output remains unknown."
  - "A raw stream previously closed its call log without accounting for the completed response. It now records usage through the same provider boundary as normal calls."
  - "Estimates and later provider receipts are not additive. They share a run-scoped physical-attempt receipt hash so analytics can prefer a measurement for each component without double counting. A provider that reuses generation ids no longer collapses distinct answers into one receipt."
---

Deploy the companion relay change in Agent-Field/website-open-infra#82 before
releasing the client. The prior relay rejects the new status values. Estimates
never enter billing, response usage or token-limit enforcement; existing opt-out
settings apply to the new count fields. The contract, limits and receipt-aware
query are documented in docs/model-usage-accounting.md.
