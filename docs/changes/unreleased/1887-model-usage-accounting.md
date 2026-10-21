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
  - "Native input_tokens/output_tokens aliases previously recorded zero counts in responses, call logs and ledgers. They now provide measured counts, including native cache reads and writes in the cache-inclusive prompt count; equivalent canonical counts yield the same billing and token price."
  - "Usage-option refusal repair previously stopped after another repair, or prevented a later reasoning repair. Independent repairs now compose once each; unrelated refusals keep their existing handling."
  - "Repeated streamed tool names and opaque replay could inflate text estimates, and metadata-only cuts could invent zero output. Names now count once per call, opaque input remains unknown, and only observed text or normal completion establishes output."
  - "Late cost-only and one-sided receipts previously fabricated measured zeroes. Recovery now preserves component presence and trimmed receipt identity without changing its price or ledger row."
---

Deploy the companion relay change in Agent-Field/website-open-infra#82 before
releasing the client. The prior relay rejects the new status values. Estimates
never enter billing, response usage or token-limit enforcement; existing opt-out
settings apply to the new count fields. The contract, limits and receipt-aware
query are documented in docs/model-usage-accounting.md.
The provider billing boundary copies a supplied cost and does not derive it from
tokens when cost is absent. Unmetered direct-service answers retain the prior
no-lookup behavior even on overflow; telemetry follows the actual attempt.
Telemetry status is derived from emitted components, unsafe counts and sums are
omitted, and estimates require a receipt identity.
