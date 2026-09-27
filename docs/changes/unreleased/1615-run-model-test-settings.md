---
kind: internal
title: Run-model tests preserve their fixture resource settings
pr: 1615
surface: [build]
invalidates:
  - "Three model-pinning tests overwrote the fixture profile and re-enabled the real machine gate. Model pins now preserve the fixture's disabled load and memory limits."
---

This removes one source of load-dependent failures tracked by #1525 without
changing product resource limits or tests that exercise those limits.
