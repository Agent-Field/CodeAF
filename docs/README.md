# furrow Documentation

Start with the repository [README](../README.md) for installation, daily use,
two-machine sync, and current implementation status.

## Product

- [Product direction](product.md) — audience, purpose, design principles, and
  Mission Control interaction guidance.
- [Performance](performance.md) — reproducible benchmark methodology, current
  measurements, regression gates, and explicitly unproven targets.

## Specifications

The numbered documents have explicit ownership boundaries. Document 1 is
authoritative when engine semantics overlap with the companion documents.

1. [Working-state engine](specs/01-working-state.md) — capture, snapshots,
   rewind, forks, merge, sync, retention, fidelity, and storage security.
2. [Distributed runtime](specs/02-distributed-runtime.md) — remote execution,
   networking, developer experience, coordination, and commercial boundaries.
3. [Parallel universes](specs/03-parallel-universes.md) — multi-agent product
   model, convergence, capture economics, CLI discipline, and UI scope.

## Repository Notes

- UI dependency notices remain at
  [ui/THIRD_PARTY_NOTICES.md](../ui/THIRD_PARTY_NOTICES.md) beside the embedded
  assets they describe.
- Runnable workflows live in [`demo/`](../demo/); benchmark code lives in
  [`benches/`](../benches/).
