# Swarm benchmark — measuring cooperative decomposition against refusal-to-split

This suite exists to answer one question honestly: **when does swarm mode
(`AFORGE_SWARM=1`, the cooperative claim-time decomposition) beat the
default refusal-first pipeline, and on which task shapes?** It is the
reusable instrument for that measurement — add a task, get a verdict; come
back after every change that touches planning, splitting, or capacity, and
re-read the suite's delta.

It follows the same doctrine as `bench/README.md`, which you should read
first. The short version, applied to this measurement:

- **The verdict is never the run's self-report.** Each category has a
  deterministic verdict script in `verdicts/` that grades the artifacts the
  task produced — tests passing, files present, sections found, callers
  migrated — and returns a number. A run that narrates a success its
  artifacts do not support scores 0, exactly as `bench/` scores a harness
  that changed no files.
- **The cost comes from the journal's `usage` table, per cell, summed.**
  Aforge self-reports usage; nothing here approximates it from an account
  credit delta. `results.csv` records it as `cost_usd` from that table only.
- **The factor varies, everything else is pinned.** Each cell is one task ×
  one arm (`AFORGE_SWARM=0` or `1`) × one seed, run in a fresh directory
  with a private store. Model, timeout, task text, and verdict are constant
  across the two arms of a task; the only difference is the flag.

## The corpus and why each task is in it

The corpus spans real workloads — not only code — because swarm's claim is
that decomposition pays *when tasks are genuinely wide*; measuring only
codegen would tell us nothing about prose, review, research, or multimodal
work, and would overstate or understate the effect depending on which
single shape we picked.

Each task is engineered to have *parallel width*: independent sub-deliverables
the decomposition can land in parallel. That is the load-bearing property —
swarm is a claim about width, so the corpus must supply width to have
anything to measure.

| task | category | width it supplies | verdict (what `verdicts/<name>` returns) |
|---|---|---|---|
| `codegen modules` | codegen | 4 independent modules | modules that import & run (0–4) |
| `bugfix repo` | bug-fixing | 3 independent bugs in one repo | test files passing (`N/total`) |
| `review diff` | code review | several independent defects to find | defect classes found (0–4) |
| `prose report` | prose | 4 independent sections + verdict | required sections present (0–5) |
| `research synthesis` | research | 5 themes + citations + open questions | themes covered (0–7) |
| `api refactor` | refactoring | 8 independent call sites | clean cutover (0/1) |
| `doc coverage` | documentation | many independent public names | docstring coverage % (0–100) |
| `image captions` | multimodal | 12 independent images | images captioned (+INDEX) |

The hard version of each is the `L` tier and is gated out below `full`
because a single cell there costs minutes; power without repetition is
noise, so the suite defaults to small tiers with more seeds instead.

## Running it — parallel by default

```
bench/swarm/run.sh                # tier=smoke: one seed per non-L task, both arms
TIER=standard bench/swarm/run.sh  # n=3 per task
TIER=full bench/swarm/run.sh      # adds the L task, n=3-5
```

The suite runs cells **concurrently**, bounded by `JOBS` (default 4):

- **Pair-fairness:** both arms of one task fire together, so a rate-limit
  wave or provider slowdown hits the OFF and ON arm of the same comparison
  at the same time and cancels out of the delta. Serial arms would
  contaminate wall-time with whichever wave came first.
- **`JOBS=1` is the timing-clean mode.** Concurrency shares provider
  throughput, which adds noise to `wall_s`. Run `JOBS=1` for the timing
  baseline; run the default parallel mode for throughput (cost and verdict
  are unaffected by concurrency).

Results append to `bench/swarm/results/results.csv`, one row per cell:
`task,category,arm,seed,wall_s,exit,cost_usd,nodes,usage_rows,verdict`.
Keep that CSV; it **is the baseline**. A re-run after a change to
`internal/plan`, `internal/resident`, or the JIT/capacity wiring is
comparable to it row-for-row because the task texts and verdict scripts are
pinned in `tasks/` — the same words and the same grade, every time.

Tiers control **power, not coverage**: `smoke` is the wiring check (do both
arms run and grade), `standard` is where a claimed delta starts to mean
something, `full` adds the large multimodal cell. Never quote a smoke-tier
number as a delta; quote it as "the arms both ran."

## The baseline

First-run numbers go in `BASELINE.md` after `TIER=standard` completes. The
comparison the suite is built to show, per task:

- **wall time** — where cooperative decomposition pays, this is the whole
  point; swarm should lose where width is genuinely absent.
- **cost_usd** — decomposition spends planning calls; swarm should pay for
  itself in wall time without inflating cost past the work it parallelizes.
- **verdict** — quality must not drop to buy speed; a fast arm with a worse
  verdict is a loss, read it that way.

A one-line `wall: ON/OFF` and `verdict: ON−OFF` summary per task belongs in
`BASELINE.md`, with the raw CSV kept beside it.

## What this deliberately does not measure

- **No LLM-judged quality.** Where a grade is not deterministic (e.g. prose
  elegance), the verdict counts structure, not taste — and the README does
  not pretend structure is elegance. It is the honest, reproducible subset.
- **No cross-harness comparison.** That is `bench/`'s job, unchanged. This
  suite varies one flag.
- **No L-tier by default.** Power needs seeds, not heroics.
