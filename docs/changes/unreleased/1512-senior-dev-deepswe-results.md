---
kind: added
title: senior-dev's DeepSWE results are in the repository, three campaigns as per-task tables
pr: 1512
surface: [docs]
invalidates:
  - "There was no committed record of how senior-dev does on DeepSWE — the only DeepSWE material in the repository was `bench/deepswe/`, the rig, whose `results/` is gitignored, so every number lived on the machines that produced it. `docs/benchmarks/deepswe/` now holds three campaigns as per-task tables: a ten-harness comparison on DeepSeek V4 Flash where senior-dev came first at 62/113, senior-dev `278a076` on DeepSeek V4.1 Flash at 88/113, and senior-dev `f3b9716` on Kimi K3 at 78/113. Each row carries reward, F2P, P2P and the test counts behind them, wall time, cost, model calls and the binary that produced it."
  - "`docs/benchmarks/` measured one thing, the CLI's own footprint, so `performance/` was its only section. It now has two, and the index says which is which: `performance/` for the footprint, `deepswe/` for task-solving."
  - "`codeaf` named exactly one binary as far as this repository was concerned. The DeepSWE artifacts arriving here were produced by a *different* binary of that name — the other Agent-Field project renamed away, first to `swe-pro` and then to `senior-dev` — so a `codeaf` in those campaign records is senior-dev's former name, not this product's. The values are rewritten to `senior-dev` throughout and the index states the collision outright."
  - "A DeepSWE figure could be read as one denominator. It cannot: `bench/deepswe/` works from the 117-task v1.1 checkout and these campaigns from the 113 *scored* tasks of the same corpus, so 113 here and 117 there are different sets."
---

The rig is not the record. `bench/deepswe/` measures this product's own `codeaf` and
writes where nothing is committed, which is right for a rig and useless for anyone
asking later what the numbers were. These campaigns ran on senior-dev's rig against
the official DeepSWE verifiers at `0b9fabb`, and what is committed is only the
outcome: 308 KB of tables, against roughly 23 GB of per-attempt artifacts left behind.

The caveats travel with the numbers rather than sitting in a footnote. Five tasks
across four arms produced no usable verifier outcome and are empty rather than zero,
while still counting against 113. Six of the nine other harnesses recorded no
per-attempt cost. And the Kimi K3 run resolves no difference against the V4.1 Flash
run — the arms differ in model, binary and coder prompt inseparably, the re-anchor
that would have separated them was never run, and the preregistered two-sided McNemar
gives p = 0.0895.
