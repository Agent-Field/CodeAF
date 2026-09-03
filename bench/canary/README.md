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
3. **A home of its own.** `AFORGE_HOME` moves the whole state root, so every
   cell has its own journal, call log, budget and first-run history. Only the
   api key is carried over from the person's profile; the talk model, all four
   tiers and the approval posture are written to the one model under test.
4. **One door.** `do`: `aforge do "<issue>" -w <clone> -json -yes-spend -model M
   -plan-model M -timeout 900`. `chat`: the binary in a tmux window standing in
   the clone, `aforge chat -yolo -one-model -model M -max-cost 1`, the issue
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
| `door_verdict` | **the door's own verdict**: `ok` (ended by itself, exit 0, no question left), `partial` (ended by itself, non-zero exit), `asked` (a question left for a person), `wall`, `stall`, `crash`, `noframe`, or empty when there is no door record |
| `tests_verdict` | **the tests' verdict**: `green`, `red`, `regressed`, `no grade` |
| `pass` | true only when both verdicts are good, the cell was set up, cost is under cap and wall under the limit — the conjunction, and what a regression is measured on |
| `reason` | the first thing that went wrong, in a reader's order |
| `ended`, `exit` | the door record's own facts, kept beside the verdict |
| `wall_s`, `cost_usd`, `ttft_ms`, `calls` | measured: the door's clock, the run's own bill, the turn's first call, the call log's count |
| `changed_files` | how many files the door left changed, before the overlay |
| `f2p`, `suite`, `regressed` | `judge.py`'s two pytest readings and the comparison against `base_suite` |
| `subharness`, `nodes` | what `do` planned; empty for `chat` |
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
