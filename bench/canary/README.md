# The canary

Can this build still finish an ordinary GitHub issue a person would paste? The
canary answers that on a schedule: a fixed pool of real issues, both doors, one
pinned model, and a grade nobody in the run gets to write. It spends real money
and is on demand — nothing in `make check` reaches it.

The scoreboard is issue #407, one comment per run. Read `PICKS.md` for how the
pool is chosen and `pool.json` for the pool itself.

## Shape

The unit is a **cell**: one issue, one door, one clone, one home, one grade.

1. **A clone with the fix out of reach.** The repository is mirrored once and
   the working tree fetches exactly the base commit — the tree the issue was
   filed against — so the history the door can read stops where the issue was
   open, and the merge that fixed it is nowhere in the tree.
2. **The suite, installed before the clock starts,** by the rung `pick.py`
   validated the pick on, so a cell builds the environment its grade was
   measured in. The venv's `bin` leads `PATH` for the door.

   The base counts a grade is read against are measured the way a cell is
   graded: the whole suite runs with `--continue-on-collection-errors`, so a
   module that cannot import is one error and the rest of the suite still
   runs. Two things can leave a base stale — a rung that stopped resolving,
   so every cell reads `venv: pip install ... failed` at 0 s, or a base
   measured before that flag existed, where one unmet optional dependency
   stopped pytest at collection and the base says nothing at all.
   `pick.py --remeasure owner/repo ...` fixes both in place: it clones the
   entry's `base`, walks the ladder from the rung the entry records and down
   from there — never up — and rewrites `install`, `base_suite` and
   `measured`. The issue, its base, its merge and its tests are untouched,
   which is what keeps a frozen anchor an anchor. A base that collected
   nothing is not a baseline, so those rows read `base ⊘` in the suite column
   instead of a comparison that would mean nothing.

   **The rung is a recipe, not a resolution.** `--group dev` is re-resolved by
   pip at every cell, so on its own it builds whatever PyPI answered that
   hour: on 2026-09-03 `pypa/virtualenv`'s dev group died with pip's
   `resolution-too-deep` in every cell of two whole tables, an hour after the
   same rung resolved in `--remeasure` with identical counts. The recipe was
   right and the resolution was weather. So the venv a base is measured in is
   frozen. `pick.py` runs `pip freeze --exclude-editable` in it and writes the
   plain `name==version` pins to `lib/constraints/<owner>__<name>.txt` — the
   project's own line dropped, because a cell installs the project from its
   checkout at `base`, and links, extras and editables dropped because pip
   refuses a constraints file that carries them. The entry records the path as
   `constraints`, and every `pip install` in a cell's rung passes `-c` on it;
   with the whole graph pinned the resolver has nothing left to search. The
   directory lives under `lib/` deliberately: `run.sh` freezes `lib/` into
   `<run>/rig` for every run, so the constraints travel with the code that
   produced the rows and a cell reaches them beside its own `cell.sh`. A
   constrained install that fails falls back **once** to the same rung
   unconstrained rather than losing the cell — and says so: `cell.json` carries
   `install_constrained: false` and the table's `why` column reads `unpinned`,
   because a cell that built an environment nobody measured must not be read
   against counts from one that was.

   **The cell builds the version the base was measured at.** The working tree
   fetches the base commit and no tags, so a project versioned from VCS
   metadata installs as a placeholder like `0.1.dev1+g1d4a338`, which cannot
   satisfy its own dependents — `pypa/virtualenv`'s dev group carries
   `pre-commit-uv`, which requires `virtualenv>=20`, so pip's resolution is
   impossible and every one of its cells read `venv: pip install --group dev
   failed`. `pick.py` records the version its own tagged clone installed as in
   the entry's `version`, and a cell exports it as
   `SETUPTOOLS_SCM_PRETEND_VERSION` and `HATCH_VCS_PRETEND_VERSION` around its
   pip installs only, never for the door.
3. **A home of its own.** `AFORGE_HOME` moves the whole state root, so every
   cell has its own journal, call log, budget and first-run history. Only the
   api key is carried over from the person's profile; the talk model, all four
   tiers and the approval posture are written to the one model under test.
4. **One door.** `do`: `aforge do "<issue>" -w <clone> -json -yes-spend -model M
   -plan-model M -timeout 900`. `chat`: the binary in a tmux window standing in
   the clone, `aforge chat -yolo -one-model -model M -max-cost 1 -max-hours H` (H is the wall minus a 60 s margin, in hours, so aforge ends on its own law before the rig clock stops watching), the issue
   pasted as a bracketed paste, enter. Nothing on the screen is trusted to say
   the turn is over: it is over when the journal holds a non-aux `usage` seal,
   the call log shows nothing in flight, the status line reads idle, and the
   call log has been quiet for twenty seconds.
5. **The grade.** The fix pull request's test files are laid over whatever the
   door left and run; then the whole suite. The door's own diff is kept beside
   the grade, taken before the overlay, because when a cell fails the diff is
   the only evidence of what the model did.

## What PASS means

All of: the pull request's tests are green on the tree the door left; the
whole suite has no more failures and errors than the pool measured at base;
the door ended on its own — `do` exited 0, settled, with no question left for
a person; `chat` sealed its turn before the wall with no crash and no stall;
cost is under the cap and wall under the limit. Anything else is a FAIL with
the first reason that applied, in the order a reader would ask: could it run,
did it end, did it work, what did it cost.

## Honesty

- **Same pool, same model, same doors, every run.** Anchors in `pool.json`
  are frozen; `pick.py` refuses to overwrite them. Fresh picks are drawn per
  run for coverage and never count as regressions: one that fails on both
  doors is first a bad pick, one that fails on one door is a finding.
- **Every number is measured.** Cost is the call log's sum over calls that
  came back — the one door every outbound call passes through, at both
  surfaces. Wall is the door's own clock. First-token latency is the turn's
  first call. Nothing is estimated and nothing is filled in.
- **Load is recorded beside every row**, the one-minute average at the start
  and end of the cell. A wall under load 100 is not a wall under load 5.
- **A flaky pass is a fail until understood.** The rig does not retry.

## Running it

The chat library is bash-only. When driving a cell by hand, set `CANARY_LIB` to
the lib directory.

```sh
bench/canary/run.sh --bin ~/af-dev/bin/aforge                     # anchors, both doors
bench/canary/run.sh --bin BIN --fresh 2                           # plus two fresh picks
bench/canary/run.sh --bin BIN --baseline RUN/rows.csv --post 407  # compare and post
bench/canary/run.sh --dry-run                                     # list cells, spend nothing
```

Evidence lands under `bench-results/canary/<stamp>-<sha>/`: `rows.csv` and
`scoreboard.md` for the run, and per cell the door's record, the screen it
ended on, the diff it made, the pytest logs, and the home it ran in.

## The interface

The contract another runner can consume. Nothing here is derived twice: a value
appears in one file and is carried, never recomputed.

**A pool entry** (`pool.json` → `anchors[]`, and a fresh pick's `picks[]`). All
of it is written by `pick.py` and validated before the entry is kept.

| field | what it is |
| --- | --- |
| `id` | the cell's name, `owner-repo-issue`; the cell directory is `<id>-<door>` |
| `repo` | `owner/name` on GitHub, the mirror this cell clones |
| `issue` | the issue number the door is asked to fix |
| `pr` | the merged pull request that fixed it; the grade comes from here |
| `title` | the issue title, for reading a table |
| `base` | the commit the issue was filed against; the tree the door gets |
| `merge` | the merge commit the test files are taken from, after the door stops |
| `test_files` | the pull request's test files — the fail-to-pass set |
| `src_files` | the files the pull request changed outside tests |
| `src_lines` | how many source lines the fix moved; the size of the ask |
| `stars` | the repository's stars when picked; a floor keeps toy repos out |
| `prompt` | what is handed to the door, verbatim — the issue, nothing else |
| `install` | the rung of `pick.py`'s ladder the suite installed on |
| `constraints` | the resolution that rung was measured under, frozen by `pick.py` as `lib/constraints/<owner>__<name>.txt`; every `pip install` in a cell passes `-c` on it. Absent means the entry predates the freeze, and its cells resolve the rung afresh |
| `version` | the version the project installed as when the base was measured, read from that venv with `importlib.metadata`; a cell exports it as `SETUPTOOLS_SCM_PRETEND_VERSION` and `HATCH_VCS_PRETEND_VERSION` for its pip installs. Absent when it could not be read, and a cell then installs with whatever version its tagless tree produces |
| `python` | the interpreter the pick was validated with |
| `original_tests` | how the pull request's test files stood before it, or a note |
| `f2p_at_base` | those tests run at `base`: they must fail there or it is no test |
| `gold` | those tests run on the merge: they must pass or the pick is unsound |
| `base_suite` | the whole suite at `base`; a regression is measured against it |
| `measured` | the day the base was last re-measured by `--remeasure`; absent means it has stood since `picked_at` |
| `picked_at` | when `pick.py` wrote the entry |
| `source` | `fresh` — the picker's own search — or `swe-bench-verified`, fed from that public list and so an issue a model may already have read. Absent means `fresh` |
| `tier` | the narrowest size band the fix fits: `small` (≤2 files, ≤150 lines) or `medium` (≤4 files, ≤400 lines). Absent means `small` |

`run.sh` writes two more into each cell's `entry.json`: `anchor` (true for a
frozen pool entry) and `door` (`do` or `chat`).

**`cell.json`**, one per cell, written by `cell.sh` and what the scoreboard
reads — beside its own `entry.json`, for the base counts. It carries `id`, `repo`, `issue`, `anchor`, `door`, `source`
and `tier` from the entry, and then:

| field | what it is |
| --- | --- |
| `door_verdict` | **the door's own verdict**. `do` exits on a five-rung ladder and the column reads it: `0` done → `ok`, `1` could not run → `could-not-run`, `2` ran and did not finish → `partial`, `3` a limit the person set — the wall, a budget, the turn cap → `wall` when the rig's own wall is what fired and `limit` otherwise, `4` needed a person → `asked`. `chat` has no such ladder and is read from its record: `ok` (sealed its turn, exit 0, no question left), `partial` (ended by itself, non-zero exit), `asked` (a question left for a person), `wall`, `stall`, `crash`, `noframe`. Empty when there is no door record |
| `tests_verdict` | **the tests' verdict**: `green`, `red`, `regressed`, `no grade` |
| `pass` | true only when both verdicts are good, the cell was set up, cost is under cap and wall under the limit — the conjunction, and what a regression is measured on |
| `reason` | the first thing that went wrong, in a reader's order |
| `ended`, `exit` | the door record's own facts, kept beside the verdict |
| `wall_s`, `cost_usd`, `ttft_ms`, `calls` | measured: the door's clock, the run's own bill, the turn's first call, the call log's count |
| `changed_files` | how many files the door left changed, before the overlay |
| `f2p`, `suite`, `regressed` | `judge.py`'s two pytest readings and the comparison against `base_suite` |
| `subharness`, `nodes` | what `do` planned; empty for `chat` |
| `install_constrained` | whether the suite installed under the entry's `constraints`: `true`, `false` when it fell back to the rung unconstrained — which the table shows as `unpinned` — and `null` when the entry names no constraints to honour |
| `load` | the one-minute load average at the start and end of the cell |

**`rows.csv`**, written by `report.py`, one row per cell. Its columns are
`run`, `sha`, `id`, `door`, `anchor`, `pass`, `wall_s`, `cost_usd`, `ttft_ms`,
`changed_files`, `f2p_passed`, `f2p_failed`, `suite_passed`, `suite_failed`,
`load`, `reason`, `door_verdict`, `tests_verdict`, `source`, `tier` — the cell
fields of the same name, plus `run` and `sha` from `run.json`, `door` as the
door used, `anchor` as `yes` or `fresh`, and the pytest counts unpacked. **A
new column is appended and never inserted**, so a reader taking the file by
position still finds every old column where it has always been.

The two verdict columns are `door_verdict` and `tests_verdict`. They are never
folded together: a door that refuses work the tests call green is a defect of
this product, and one column cannot say it. `pass` is their conjunction and
exists for the baseline comparison, not for reading. A run
compared with `--baseline` reports a regression on **anchors only**.
