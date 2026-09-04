# Pilot 01 — the first live cells, and the cost that cannot be used

Run by the owner in their own environment on commit `4205ecff1`, print door
only, one scenario (`data-tally`), three arms, effort `low`, pinned to
`deepseek/deepseek-v4-flash-0731` through `lib/guard.py`. The raw evidence
directory from that run is kept as it was written; nothing here overwrites it,
and this file is the analysis beside it rather than an edit of it.

## What it showed

All three cells passed their quality assertions, and the guard's audit log named
the pinned model and no other — which is the first end-to-end evidence that the
open-model policy holds through three different CLIs at once.

| arm | wall | reported cost | tokens in/out |
|---|---|---|---|
| aforge | 9s | $0.00170041956 | 32724 / 649 |
| pi | 7s | **$0.0 — not usable** | 16325 / 637 |
| omp | 9s | $0.00378749 | 49514 / 886 |

## Why the pi figure is invalid, and why the other two are not comparable to it

Each cost above is the harness's own arithmetic over its own price table. To
route pi through the guard the rig writes a custom provider into `models.json`,
and that entry then carried explicit zero price fields. pi did what it was told:
it multiplied 16,962 real, billed tokens by zero and reported $0.0. Nothing was
free. A frontier built on that number would have put pi at the cheap end of it
for having been misconfigured by the benchmark.

Two changes followed, and both are in the rig now:

1. The rig no longer writes a price table into pi's provider entry at all. It
   was fabricated data in a file the harness trusts.
2. Cost is measured at the guard, from the provider's own `usage`, for every arm
   and every call including auxiliary ones. `receipts.py` prefers that figure
   (`cost_source: guard-upstream`) and falls back to self-reported only when
   upstream returned nothing.

As a backstop, a self-reported `$0` beside a non-zero token count is now
recorded as **unknown** rather than zero, and the cell is marked not comparable.
The selftest exercises exactly that case.

The aforge and omp figures above are internally plausible but were produced the
same way — from each harness's own price table, not from the provider — so this
pilot supports **no cost comparison at all** between the three arms. It supports
the quality result, the wall clocks, and the model-policy result. The first
comparable cost numbers will come from a run whose costs carry
`cost_source: guard-upstream`.

## Also not established by this pilot

The interactive door was not exercised: these were all `--print` cells. Nothing
here says anything about what any of these harnesses does with a message typed
while work is running.
