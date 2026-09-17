---
kind: added
title: Three pure packages — a crew picker, an additive tally and a measurement-index reader
pr: 1093
surface: [engine]
invalidates: []
---

Standard library only, no disk, no network, and nothing on a surface wired to
them yet.

- `internal/crewpick` scores every (worker, high, mastermind) combination a
  candidate list can field by the bill its seat volumes run up against the mean
  of its seat qualities, and reads the frugal, balanced and max picks — and a
  knob between them — off the non-dominated front.
- `internal/pool/tally` keeps per-address sufficient statistics (n, sum, sum of
  squares) and paired-comparison counts, so two sheets merge by addition in any
  order and marshal to a byte-identical schema-1 document.
- `internal/pool/index` parses a schema-versioned JSON document of per-model
  measurements into an immutable value every goroutine can share, resolving
  model aliases and ignoring fields, metric kinds and dims it does not know.
