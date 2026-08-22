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

Everything is kept: the harness log, the pytest log, the base commit, the
completed graph, the kept store where there is one, and a CSV row per cell in
`results.csv`.

## The CSV

```
harness,issue,seconds,exit,changed_files,passed,failed,cost_usd,cost_source,aforge_mode,subharness_chosen,nodes_failed
```

The last three columns are new and are **appended, not inserted**. A CSV written
before they existed is still a valid CSV, and a reader that indexed the first
nine columns by position still reads the same nine things. Old rows simply have
no value for the new two; read them as `node` and `linear`, which is what every
recorded aforge row was.

- `aforge_mode` — the shape the cell ran in (below). `n/a` for pi and opencode,
  which have one shape and no name for it.
- `subharness_chosen` — which worker actually took the leaf. For the forced
  shapes it is read back out of the completed graph rather than assumed; for
  `select` it is read out of the run's own store. `unknown` means the run left
  nothing legible to read, and that case is real — see below.
- `nodes_failed` — failed nodes counted out of `done.json`, because the exit
  code is not the verdict: a smoke run watched the engine crash inside its
  leaf while `run` exited 0, over a suite that was green before the harness
  arrived. A row with `nodes_failed > 0` is a DNF whatever its other columns
  say. `n/a` for pi, opencode, and `select` (no `done.json` to read).

## Cost: harness self-reporting only

**The cost column is filled in from the harness's own reported usage, never from
an account-level credit delta.** For aforge that is the `$` figure on the run
summary line, which comes from the provider's per-response usage accounting
summed over the run.

That one line is the source in all three aforge shapes and the script parses it
the same way in each. `aforge run` ends with it; `aforge do` ends with
`<elapsed> · <n> nodes · $<spend>`, which is the same accounting through a
different mouth. A `swe` leaf's spend arrives from the engine's own terminal
event, lands in the leaf's `Outcome.Usage`, and is summed into that figure like
any other leaf's — so nothing in the cost path knows or needs to know which
worker ran. The shared-key law is unchanged by the new shapes: self-reported or
nothing.

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

Four shapes, selected with `AFORGE_MODE`. The first three are the same issue
taken three ways, against the same recorded pi and opencode rows.

- `node` (default) — the one-node graph in `graphs/issue.json`, rendered with
  the issue text as the leaf's brief, executed on the default worker. This
  measures the executor alone: one agent, one leaf, no planning call. It is the
  shape quoted in the issue matrix, because it is the like-for-like comparison
  against pi and opencode, which are also single agents.
- `swe` — the same one-node graph, with the leaf handed to the `swe` worker
  (`AFORGE_SUBHARNESS`, default `swe`). Same template, same single invocation,
  same clone as the workspace; the only difference from a `node` cell is one
  field on one node.
- `select` — `aforge do "<issue text>"` with nothing forced. The compiler
  decides both the shape of the work and the worker that takes it.
- `pipeline` — `aforge plan --brief` first, then `aforge run` over the resulting
  graph. This is the parallel shape and is what the PR-review comparison used.
- `chat` — `aforge chat --once "<issue text>" --yolo --one-model`. The chat
  surface's brain, one turn, nobody watching.

The workspace is the clone itself (`-w` in every shape, including `do`'s), so
the agent's writes are the diff being measured — except `chat`, which has no
`-w` and takes the process's directory, so that one shape is run from inside
the clone.

### The `chat` shape is answering a different question

The other four modes are ways of running an *errand*. `chat` is one
conversational **turn**: no graph is compiled, so there is no delivery gate, no
replan, no `done.json`, and the `subharness_chosen` column reads `n/a` because
there was no worker to choose. Structurally it is closest to `node` — one
agent, one invocation — and it is the row to quote when the question is "what
would a person typing this into chat have got?", not "how well does the
compiler decompose this?".

Three things about the cell are the command's shape rather than a choice:

- **`--yolo` is not optional.** Nobody is watching, so consent is refused rather
  than assumed, and a cell without it changes zero files while looking healthy.
- **`--one-model` is not optional either, and this is the subtle one.** A chat
  session resolves its auxiliary calls — titles, safety, compaction, the check
  on finished work — through the crew rows and role pins in the operator's
  `/settings`. `-model` alone therefore measures *that machine's profile* as
  much as the named model: on one trivial task, 22% of the spend went to a
  model the run never named. `--one-model` settles every text call on the
  session model for that run without writing any setting.
- **`AFORGE_HOME` is set per cell.** Chat keeps its state in the shared home;
  four parallel cells sharing one would be four writers on one store, and the
  cells would leak into each other and into the operator's own history.

Its cost is not read from a summary line, because `--once` does not print one —
it ends with the reply. The spend is summed from the `usage` records in the
cell's own session transcript, one per model, **including the ones marked
`aux`**: those are exactly the calls `--one-model` exists to make legible, and
a reader that skips them under-reports.

Substitution into the graph template goes through `python3` rather than `sed`:
issue bodies contain quotes and newlines that would otherwise produce a graph
file that does not parse.

### Forcing the worker: the field, not the flag

`swe` mode sets `"subharness"` on the rendered node rather than passing
`--subharness` to `aforge run`. Both levers exist and `run --subharness` writes
the same field into the same place, so they are the same instruction — but the
field keeps the invocation byte-identical between `node` and `swe`, which is
what makes the two cells comparable. The difference between them is one JSON
key, and it is visible in the graph file kept with the cell.

The same reasoning keeps `node` mode untouched: rendering with no worker named
writes no field at all, so the graph a `node` cell executes today is the graph
it executed before these modes existed.

### The comparison doctrine

The three shapes answer three different questions and only make sense read
together:

- **`node` is the drift control.** It is the recorded configuration, unchanged.
  If a `node` re-run does not land near the numbers in `../BENCHMARKS.md`, the
  harness moved, and every other row in the same grid is suspect until that is
  explained. It is measured first for that reason and not because it is
  expected to win.
- **`swe` is the specialist forced.** It answers "what can this worker do on
  this issue", which is not the same question as "would it have been chosen".
  Forcing is how a worker is measured rather than trusted; a specialist that
  only ever runs when something else decided to call it can never be told apart
  from the decision to call it.
- **`select` is the shipping claim.** It is the only row that reflects what a
  person actually gets, because it is the only one where nobody put a thumb on
  the choice. A `select` row is not a third harness — it is `node` or `swe` plus
  the decision, and `subharness_chosen` says which.

**pi and opencode are the bar**, not the baseline. Their recorded rows in
`../BENCHMARKS.md` are what any aforge shape has to beat to have shown anything;
beating `node` with `swe` shows only that aforge has two workers.

The honest expectation on these four issues is that **`select` routes to
linear**, and that is a pass rather than a failure. `swe`'s own registered
purpose says not to choose it when the change is one obvious edit, and #20 and
#23 are exactly that — a workflow file and a dependency guard, done in under two
minutes by all three harnesses. A compiler that reached for the specialist on
those would be choosing badly. What would be worth reporting is `select`
matching `node` on the small issues and reaching for `swe` on the large ones,
and the four issues here are probably not enough to show the second half.

### Reading `subharness_chosen`

- Forced shapes read it back out of `done.json`, the completed graph the run
  wrote. If the run died before writing one, the column records what was asked
  for, which is the most that is known.
- `select` reads it out of the run's own store. `aforge do --keep` leaves its
  private store behind and prints where; the node row there carries the settled
  choice, which is the same durable field the graph shapes set by hand. The
  script moves that store into the cell directory afterwards, so it is kept with
  the rest of the cell's evidence. This needs `sqlite3`; without it the column
  reads `unknown` and nothing else changes.

**The observability gap.** Nothing on stdout or stderr names the chosen worker —
not `do`'s progress lines, not its `--json` object, not the run summary. The
store is the only place outside the process where the choice is legible, which
is why this column costs a `--keep` and a `sqlite3` query rather than a `grep`.
If `do --json` ever grows a `subharness` field this should read that instead.

There is a second gap, and it belongs to the forced shapes. A build with no
`swe` worker registered degrades the leaf to the default one — that is the
registry's promise, degradation rather than failure — and it does so silently
when the worker is named in the graph, so a `swe` cell on such a build is a
`node` cell wearing the name: same argv, same shape, same silence, and a CSV row
that says `swe`.

Nothing lists a build's registered workers from outside the process, so there is
no free pre-flight check. Two after-the-fact ones:

- `~/.aforge/profile-<model>-swe.json` exists only if a `swe` leaf really ran,
  because a measurement is filed under the worker that took the leaf and not the
  one that was asked for. No file, no `swe`.
- A `swe` and a `node` cell that come back with identical turn counts and
  identical costs did not run different workers.

The flag form (`aforge run --subharness swe`) does print
`no subharness named "swe"` on stderr, but only once the run is already
underway, so it is evidence in the harness log rather than a check you can make
before spending.

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

The cap is one cap. `aforge do` carries its own wall in seconds and defaults to
fifteen minutes, so `select` cells are given `-timeout` converted from
`CELL_TIMEOUT`: a shape held to a quarter of the time the others get is not the
same cell.

## The dry run

```sh
AFORGE_MODE=swe ISSUES=21 HARNESSES=aforge bench/run.sh --dry-run
```

`--dry-run` (or `BENCH_DRY_RUN=1`) composes every invocation and executes none
of them. It clones nothing, builds no venv, calls no model, and prints the exact
argv the real run would launch — plus, for the graph shapes, the rendered graph
and the worker in it. It is the check for the failure that costs the most to
find any other way: a flag that moved between versions, whose first evidence is
a grid of zero-file cells and a bill.

The one thing it still reaches out for is `gh issue view`, because the issue
text is an input to the composition. If that fails it composes with a stand-in
of the same shape and says so, so the wiring can be checked offline.

## Running it

```sh
REPO=https://github.com/MALIBA-AI/bambara-text-normalization \
MODEL=deepseek/deepseek-v4-flash-0731 \
ISSUES="20 21 22 23" \
HARNESSES="aforge pi opencode" \
AFORGE_MODE=node \
BASE_COMMIT=6c978ffa1c49ba600c85eb893958409e37dbedd2 \
bench/run.sh
```

The three-way aforge comparison is three runs of that with `AFORGE_MODE` set to
`node`, `swe`, and `select`, each writing its own `results.csv`.

Two lines of that invocation are load-bearing honesty:

- **`BASE_COMMIT` pins every clone to 2026-08-02** — the last commit with all
  four issues still open. The repository has since merged fixes for #23
  (2026-08-06) and #21 (2026-08-08), so an unpinned clone passes the suite
  before any harness runs and every row on it measures nothing. The recorded
  pi/opencode rows predate those merges; only pinned reruns are comparable to
  them, and even then suite drift means within-row comparison beats
  cross-table comparison.
- **`MODEL` is passed to aforge as `-model` on every invocation** (run, plan,
  do), because the CLI flag is the only rung that outranks the picker
  preference in `~/.aforge/settings.json` — an env var does not. The smoke run
  found this the honest way: settings resolved to a `-latest` alias the
  engine's catalog rejected, and the cell died at $0 while claiming exit 0.

Requires `git`, `python3`, `gh` (authenticated), and `timeout` (`gtimeout` from
coreutils on macOS is picked up automatically); `sqlite3` is optional and is
only what fills in `subharness_chosen` for `select` cells.
`OPENROUTER_API_KEY` must be set for aforge — it is the key the engine inside a
`swe` leaf runs on as much as the one every other leaf runs on, and the script
exports what the shell already has rather than defining a key of its own. It
says so early when there is nothing to export. The other harnesses need whatever
their own configuration expects.

## What this protocol does not establish

One repository, one model, one attempt per cell. Test-count deltas measure
whether a change works against a suite that already existed; they say nothing
about whether the change is the one a maintainer would have written. Read the
caveats section of `../BENCHMARKS.md` before generalising from any of it.
