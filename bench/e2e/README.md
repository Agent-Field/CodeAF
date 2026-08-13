# End-to-end regression battery

Seven task shapes, run end to end against a real model, with the run's own
journal opened afterwards and checked. `run.sh` runs it; this file explains what
each cell guards and which numbers it is honest to quote.

This is the permanent form of the probes that were run by hand during the
tasker work. The point of writing them down is that a probe you run once tells
you the state of the world; a probe you can run again tells you whether you
changed it.

**This spends real money and is on demand.** It is not wired into `make check`
and must not be. `make check` runs before every commit; this runs when you have
changed the tasker and want to know what it cost you.

## Shape

The unit is a **cell**: one task shape, one harness, one scratch directory.

Each cell:

1. **Generates its fixture.** Deterministic, offline, from a script in
   `fixtures/`. No clone, no network, no `go mod download` — the Go fixtures are
   stdlib-only and their `go` directive is old enough not to pull a toolchain.
   A fixture that no longer generates is caught by `--dry-run`, before any spend.
2. **Runs one harness invocation** with the task text as the whole instruction.
   Every arm receives the same text; the only difference between cells is who
   executes it.
3. **Autopsies the journal.** `aforge do --keep` leaves a SQLite store behind;
   the store is moved next to the cell's logs and then read for node count,
   route, edge structure, leaf start times, growth events and summed cost.
4. **Asserts.** Each cell emits `✓`/`✗` lines and one CSV row.

The store is moved *before* it is read, for two reasons. Left in the system temp
directory it gets swept before anyone looks at it; and read where the engine put
it, it is a WAL database whose sidecars may still need recovery — which a
read-only open cannot do, and which fails by **answering nothing rather than by
erroring**. That failure mode cost a debugging cycle during this battery's own
construction: a perfectly healthy run was autopsied as zero nodes and no route.

## The model pin, and why it never changes silently

Two spellings of one model:

| arm | slug | why |
|---|---|---|
| aforge | `~deepseek/deepseek-v4-flash-latest` | aforge's own alias syntax, and its shipped default (`internal/config/config.go`, `DefaultModel`) |
| pi, opencode | `deepseek/deepseek-v4-flash-0731` | the concrete OpenRouter id that alias serves (`bench/probelab/REPORT.md`) |

The aforge arm is given the alias rather than the concrete id on purpose: the
battery measures the tasker **as configured**, not as specially tuned for a
benchmark.

The peer arms cannot be given the same string. The leading `~` is aforge's, not
OpenRouter's, and passing the alias to pi returns
`400 ... is not a valid model ID` — verified, not assumed. So the peer arms are
given the concrete slug, and the runner **refuses to run an arm whose model it
cannot pin exactly**: `pi --list-models` is consulted for an exact field match,
and a miss records a `skipped` row carrying the reason instead of a comparison.

This matters because the quietest way this battery could lie is to run pi on a
neighbouring model and put both rows in one CSV. Three things guard against it:
the preflight above, the `model` column on every row, and a warning from
`compare` when one harness has rows on more than one model.

**Changing either slug invalidates every historical row.** Comparability is the
only thing a baseline has. If you must change it, say so in the commit message
and treat the rows before and after as two different files.

## The cells

| cell | task | what it guards |
|---|---|---|
| `lookup` | one-paragraph factual ask | exactly 1 work node, plan graph of 1 node, `route=single_leaf`, no edges, no growth, wall < 60s |
| `compare3` | 3 databases × 3 axes, to a file | every subject and every axis present, each axis discussed at least once per subject; **shape recorded, not asserted** |
| `fanin6` | 6 languages → ranked table | ≥6 leaves started within 2s, a sink gated on ≥6 edges, and **all 6 present in the table's rows** |
| `bughunt` | Go fixture, planted distant-cause bug | exit 0, `go test ./...` green, both test files byte-identical, the fix landed on the cache-key path, CLI prices two tiers differently |
| `feature` | currency threaded through the same fixture | exit 0, suite green, existing tests untouched, test-function count grew, `-currency` flag works for both currencies and is stable across calls |
| `bundle3` | 3 independent packages in one ask | `route=bundle`, ≥3 parts, 3 leaves within 2s, a sink on ≥3 edges, all three packages exist with their own tests and no cross-imports |
| `dynamic` | 3 pipelines, one quadratic | exit 0, suite green, dedupe ≥10× faster with an unchanged record count, report on disk **and** its content on stdout, plan staged over ≥2 stages |

### Two cells worth reading twice

**`fanin6` is the dependency-pot sentinel.** The pot of finished work handed to a
gated node was once capped at 4 KiB. Six sections of prose do not fit, so the
sink was fed four of them and never told about the other two — then wrote a
confident, well-formed, four-row table. No node failed. The delivery gate passed
it. The only evidence was the row count.

So this cell checks the **table's rows**, not the document. All six sections
*were* present in the file; it was the table, built from the pot, that had four
rows. A whole-document check would have passed the broken run.

**`dynamic` carries the `revise.go` rootID sentinel.** That bug wrote a correct
report to disk and returned nothing to the caller: the file was there, the run
reported success, and stdout carried only ceremony. Every file-based assertion
in this battery would have passed it. So the cell asserts the report exists
*and* that its content reached stdout — three ways: the stage name appears,
stdout is more than a summary line, and at least one substantial line from the
file is found verbatim in stdout.

### What is recorded rather than asserted

`compare3` records its route and node count and asserts neither. One capable
leaf writing all nine cells and three leaves writing a column each and merging
are both correct answers, and a battery that demanded one of them would fail on
a healthy improvement to the compiler.

This is the general discipline: anything with a legitimate range is recorded, so
that a change in it is *visible in the CSV* without being a failure. A battery
that fails on a healthy run stops being read, and a battery nobody reads guards
nothing.

### Calibration status

Six cells assert only things that follow from the store schema or that were
verified live. **`bundle3` is the exception**: its route assertion depends on the
compiler reading that particular sentence as a bundle, which is a model's
judgement and has not been checked against a real run.

Treat the first `bundle3` run as calibration. If it routes `planned` with three
leaves, the work is still correct and the honest fix is to relax the route
assertion to a `record` — **not** to reword the task until it produces the
answer the battery wanted.

## Cost

Self-reported usage only, summed from the store's `usage` table — the same
figure `do` prints, and the same discipline `bench/README.md` sets out at
length. **pi and opencode have no cost figure**: neither self-reports, and on a
shared key an account-level credit delta is a different quantity, not a noisy
version of the right one. Their rows read `n/a`, not a guess.

Measured: the `lookup` cell costs about **$0.002** and takes 15-25s. The six
remaining cells are much larger — four of them run a coding agent against a Go
module — so budget on the order of **a few dollars** for a full sweep, and check
`--dry-run` first.

## Running it

```sh
bench/e2e/run.sh                          # every cell, aforge arm
bench/e2e/run.sh --cells lookup           # one cell — the cheap machinery check
bench/e2e/run.sh --cells fanin6,dynamic   # a subset
bench/e2e/run.sh --arm pi                 # the same tasks through pi
bench/e2e/run.sh --dry-run                # compose everything, run nothing
bench/e2e/run.sh compare                  # latest vs previous, per cell
```

Requires `bash`, `sqlite3`, `python3`, `go`, and `timeout` (`gtimeout` from
coreutils is picked up automatically). `OPENROUTER_API_KEY` must be set.

`bin/` is gitignored, so **a fresh git worktree has no aforge binary of its
own**. `AFORGE_BIN` defaults to `<repo>/bin/aforge` and falls back to `PATH`; in
a worktree, point it at the checkout's build:

```sh
AFORGE_BIN=/path/to/aforge-v2/bin/aforge bench/e2e/run.sh --cells lookup
```

Nothing here builds aforge. Which build is being measured is the caller's
decision, and building belongs in `make check`.

### Knobs

| variable | default | meaning |
|---|---|---|
| `E2E_MODEL` | `~deepseek/deepseek-v4-flash-latest` | the aforge arm's model |
| `E2E_PEER_MODEL` | `deepseek/deepseek-v4-flash-0731` | pi and opencode's model |
| `AFORGE_BIN` | `<repo>/bin/aforge`, then `PATH` | the binary under test |
| `PI_BIN`, `OPENCODE_BIN` | `pi`, `opencode` | peer harnesses |
| `CSV` | `bench-results/e2e.csv` | the append-only history |
| `RUN_DIR` | `bench-results/e2e/<timestamp>` | logs, stores and fixtures per run |

## Results

One row per cell per run, appended to `bench-results/e2e.csv` and **never
rewritten** — a battery whose baseline file gets recreated has no baselines.

```
date,git_sha,cell,harness,wall_s,cost_usd,nodes,route,quality_pass,notes,model,exit
```

The last two columns are appended rather than inserted, the same discipline
`bench/run.sh` follows: a reader that indexes the first ten by position still
reads the first ten.

`notes` carries every `record` a cell made, semicolon-separated — the measured
values that are not assertions. It is where the shape of a `compare3` run, the
speedup a `dynamic` run achieved, and the model actually billed all live.

`compare` reads that file and prints the latest run against the previous one per
cell, and against the latest peer rows. Its thresholds are deliberately
asymmetric:

- **cost: 25%.** Cost is a sum of billed tokens and moves when the run does more
  work. Two back-to-back `lookup` runs came in 10% apart.
- **wall: 75%.** Wall clock carries queueing and provider latency too. Those
  same two runs took 15s and 24s — a 60% swing between two runs of *identical
  code*. A threshold under that reports a regression every time you run the
  battery twice, and then nobody reads it. Real wall regressions in this
  engine's history have been multiples: the `abcc5a5` window-sizing defect was
  3.2×, which clears any threshold in this range.

## Adding a cell

Create `cells/<name>/cell.sh` defining:

| name | kind | meaning |
|---|---|---|
| `CELL_TASK` | variable | the whole instruction, verbatim |
| `CELL_BUDGET` | variable | seconds, passed to `do --timeout`; a spend backstop |
| `CELL_WALL_CEILING` | variable | the wall this cell *asserts*, which is not the same thing |
| `CELL_GUARDS` | variable | one line, printed in the header and the table above |
| `cell_fixture <dir>` | function | build the fixture; must be deterministic and offline |
| `cell_check <dir> <stdout> <stderr> <exit>` | function | quality checks — run for **every** arm |
| `cell_shape <db>` | function | journal checks — aforge only |

Then add the name to `ALL_CELLS` in `run.sh`.

The `cell_check` / `cell_shape` split is the important part. Quality is
something every harness can be held to, so pi gets the same bar: tests run,
files read, CLI invoked. Shape is a question only a journal can answer, so it is
asked only where there is one.

Use `check_*` for things that must hold and `record` for things worth knowing.
When in doubt, `record` — see "What is recorded rather than asserted" above.

## What this does not establish

One model, one attempt per cell, seven task shapes chosen because they were the
shapes that broke. A green sweep says the failures this battery has seen before
have not come back. It does not say the tasker is correct, and it says nothing
about task shapes nobody has thought to write down yet.
