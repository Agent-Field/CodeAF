# Benchmark harness

The protocol used to compare aforge against pi and opencode on real issues in a
real repository. `run.sh` runs it; this file explains why each step is there and
which numbers it is honest to quote.

Measured results are in [`../BENCHMARKS.md`](../BENCHMARKS.md).

## Shape

The unit is a **cell**: one harness, one issue, one clone. The grid is every
harness crossed with every issue in `ISSUES`.

Each cell runs:

1. **Fresh shallow clone.** Every cell starts from the same commit in a
   directory of its own. Reusing a checkout leaks the previous harness's diff
   into the next harness's starting state, which flatters whoever runs second.
2. **venv and test install.** `python3 -m venv`, then the repository's own dev
   extras, falling back to `requirements.txt` and finally bare `pytest`. This
   happens *before* the harness runs so that no harness spends part of its wall
   clock on environment setup that another harness got for free.
3. **One harness invocation** with the issue text as the whole instruction. All
   three harnesses receive the same text, fetched from GitHub with `gh issue
   view`. The only difference between cells is who executes it.
4. **Changed-file count**, from `git status --porcelain`. A harness that
   finishes having changed zero files did nothing, whatever its log narrates.
5. **Full-suite pytest**, run after the harness has stopped. Its counts, not the
   harness's self-assessment, are the verdict. The prompt tells the harness not
   to weaken tests; the diff is there to check whether it obeyed.

Everything is kept: the harness log, the pytest log, the base commit, and a CSV
row per cell in `results.csv`.

## Cost: harness self-reporting only

**The cost column is filled in from the harness's own reported usage, never from
an account-level credit delta.** For aforge that is the `$` figure on the run
summary line, which comes from the provider's per-response usage accounting
summed over the run.

This is not a stylistic preference. The API key used for these runs is shared,
and account-level deltas measured across a benchmark window included up to $116
of unrelated and runaway traffic that had nothing to do with the cell being
timed. Any protocol that subtracts two credit readings around a run is measuring
that traffic too. On a shared key the delta is not a noisy version of the right
number — it is a different number.

The consequence is that **pi and opencode have no cost figure here.** Neither
self-reports usage, so on a shared key their cost is not measurable at all. It
becomes measurable only on a key isolated to a single run, and until someone
does that, comparing aforge's self-reported dollars to a pi or opencode credit
delta compares two different quantities. `results.csv` records this explicitly:
the `cost_source` column reads `self-reported` or `not-self-reported`, and the
`cost_usd` column is `n/a` in the latter case rather than a guess.

## aforge invocation

Two shapes, selected with `AFORGE_MODE`:

- `node` (default) — the one-node graph in `graphs/issue.json`, rendered with
  the issue text as the leaf's brief. This measures the executor alone: one
  agent, one leaf, no planning call. It is the shape quoted in the issue matrix,
  because it is the like-for-like comparison against pi and opencode, which are
  also single agents.
- `pipeline` — `aforge plan --brief` first, then `aforge run` over the resulting
  graph. This is the parallel shape and is what the PR-review comparison used.

The workspace is the clone itself (`-w`), so the agent's writes are the diff
being measured.

Substitution into the graph template goes through `python3` rather than `sed`:
issue bodies contain quotes and newlines that would otherwise produce a graph
file that does not parse.

## pi and opencode invocation

`PI_BIN`/`OPENCODE_BIN` and their flags are variables at the top of `run.sh`.
Both CLIs move their flags between versions. If a cell comes back with a zero
exit and zero changed files, check the invocation against the installed version
before concluding the harness failed the task — that failure mode looks
identical to a real DNF in the CSV and only the harness log tells them apart.

## Timeouts

`CELL_TIMEOUT` (default 40m) caps each harness invocation. It is a spend
backstop, not a work limit: a harness still running at the cap has produced no
diff and is recorded as a DNF, and on a metered key leaving it running is how a
benchmark run becomes an unrelated bill.

## Running it

```sh
REPO=https://github.com/MALIBA-AI/bambara-text-normalization \
MODEL=deepseek/deepseek-v4-flash-0731 \
ISSUES="20 21 22 23" \
HARNESSES="aforge pi opencode" \
bench/run.sh
```

Requires `git`, `python3`, `gh` (authenticated), and `timeout` (`gtimeout` from
coreutils on macOS is picked up automatically). `OPENROUTER_API_KEY` must be set
for aforge; the other harnesses need whatever their own configuration expects.

## What this protocol does not establish

One repository, one model, one attempt per cell. Test-count deltas measure
whether a change works against a suite that already existed; they say nothing
about whether the change is the one a maintainer would have written. Read the
caveats section of `../BENCHMARKS.md` before generalising from any of it.
