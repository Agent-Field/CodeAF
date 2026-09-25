# DeepSWE — senior-dev

Task-solving outcomes for **senior-dev**, the coding harness vendored into this
product, measured on the DeepSWE v1.1 corpus. One directory per campaign, tables only.

| Run | What | Headline |
|---|---|---|
| [`2026-09-12-harness-comparison-v4-flash`](2026-09-12-harness-comparison-v4-flash/) | Ten harnesses, DeepSeek V4 Flash, 113 tasks each | **senior-dev 1st — 62/113** |
| [`2026-09-15-senior-dev-278a076-v41-flash`](2026-09-15-senior-dev-278a076-v41-flash/) | senior-dev `278a076`, DeepSeek V4.1 Flash | **88/113 — 77.9%** |
| [`2026-09-22-senior-dev-f3b9716-kimi-k3`](2026-09-22-senior-dev-f3b9716-kimi-k3/) | senior-dev `f3b9716`, Kimi K3 | **78/113 — 69.0%** |

Each directory holds a `README.md` with the run's setup and results, and a per-task
table: one row per task carrying reward, F2P, P2P and the underlying test counts,
wall time and timestamps, cost, model calls, exit status, and which binary and model
produced it.

The two single-arm runs each have one `tasks.csv`. The comparison has one table per
harness under `tasks/`, 113 rows each, plus an `arms.csv` summarising the ten.

Nothing else is committed. The per-attempt artifacts — `events.ndjson`, `run.log`,
traces, verifier stdout, patches — come to roughly 23 GB across the two 2026-09
campaigns and stay on the machines that produced them.

## Two `codeaf`s — read this before the tables

The artifacts behind these runs were produced by a binary that **used to be called
`codeaf`, and is not this repository's `codeaf`.** Two Agent-Field projects built a
binary of that name; the other one renamed away, first to `swe-pro` and then to
`senior-dev`, which is the name used throughout this directory. Wherever the history
below says `codeaf`, it means senior-dev's own former name.

## Relationship to `bench/deepswe/`

[`bench/deepswe/`](../../../bench/deepswe/) is this repository's own DeepSWE rig,
which measures **this product's** `codeaf` binary and writes to a gitignored
`bench/deepswe/results/`. **These campaigns did not come from it.** They were run on
senior-dev's own rig, against the official DeepSWE verifiers at `0b9fabb`, and are
reproduced here as the committed record. Each run's README names its manifest,
preregistration and reproduction command.

Corpus note: the two rigs count differently. `bench/deepswe/` works from the v1.1
checkout of `github.com/datacurve-ai/deep-swe` at 117 tasks; these campaigns use the
**113 scored** tasks of that corpus. A 113 figure here and a 117 figure there are not
the same denominator.

## Reading the grades

`reward` is the DeepSWE verifier's binary pass. `f2p` is the fraction of
issue-specific fail-to-pass tests that pass after the patch; `p2p` is the fraction of
pre-existing pass-to-pass tests still passing. An empty `reward` means the task
produced no usable verifier outcome — it stays in the 113-task denominator and
contributes zero. Published F2P and P2P figures are macro-averages over all 113
scheduled tasks on the same basis.

## Binary identities

These commit SHAs belong to senior-dev's repository, not to this one. Its branch
history was re-authored on 2026-09-15, so the same tree carries two SHAs and
different documents cite different ones. Each pair below was confirmed identical by
tree hash, not by assertion:

| Recorded by the rig | After re-authoring | Tree |
|---|---|---|
| `8ae964c` | `45df90b` | `2fdf00a94890` |
| `278a076` | `712d980` | `3832b6376029` |

Each run directory is named for **what the rig recorded at the time**, so the
artifacts match their own provenance. The Kimi K3 campaign's binary `f3b9716` sits
two commits after `712d980`: `4c3084f` renamed the namespace, `f3b9716` made the
control plane optional.

## A note on names

senior-dev has been renamed twice: `codeaf`, then `swe-pro`, now `senior-dev`. The
2026-09-12 and 2026-09-15 campaigns ran under `codeaf`, the 2026-09-22 campaign under
`swe-pro`. The values here are rewritten to the current name throughout. The originals
on the run machines are untouched, and checksums recorded inside these runs describe
those originals.
