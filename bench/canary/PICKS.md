# How the canary's issues are picked

`pick.py` chooses real, closed GitHub issues whose fix is graded by the tests the
fix itself shipped, and validates every pick offline before it is admitted.
`pool.json` holds the result: the criteria as data, and the frozen anchors with
every number that was measured while admitting them.

## The mechanism

A candidate is a closed issue in a public Python repository that GitHub records
as closed by a merged pull request. The pull request's merge commit is the
**fixed** tree; its first parent is the **base** — the tree the issue was filed
against — for a squash and a true merge alike. The pull request's test files,
checked out onto base, are the grader: they fail without the fix and pass with
it. A harness is pointed at base, handed the issue text, and graded by those
tests. No model opinion enters the grade.

## The criteria, and why each exists

- **Closed by a merged pull request, in the same repository.** The grade is
  built from that pull request's diff. GitHub will happily report an issue as
  closed by a pull request in a sibling repository (an `astral-sh/ty` issue
  closed from another repository was the first thing the picker met); such a
  fix cannot be graded in the tracker's clone, so it is refused.
- **One or two changed non-test `.py` files, at most 150 changed source lines,
  no renames, no deletions.** This is the size of a five-to-ten-minute task.
  A rename or deletion means the fix reshaped the tree, and reshaping is not
  what the canary measures.
- **At least one changed test file** (path contains `test`). Without one there
  is nothing to grade with.
- **No packaging or CI changes** (`setup.py`, `pyproject.toml`, `requirements*`,
  `tox.ini`, `.github/`, `conftest.py`, ...). A harness given the source tree
  cannot be expected to reproduce a dependency bump, and a changed `conftest.py`
  changes the grader itself. Documentation and changelog fragments are allowed
  and not counted as source.
- **Issue body of 150 to 4000 characters that is not an unfilled template.**
  Shorter is not enough to work from; longer is a design document, not an issue
  a person pastes. A body with two or more empty form sections was filled in by
  a template, not written for a reader.
- **Repository not archived, at least 30 stars.** The stars floor is not a
  quality judgement; it is the filter that removes the flood of one-author
  repositories in the recently-updated stream (159 of the 371 search rows on
  the first run). Thirty is low on purpose: the canary wants ordinary projects.
- **The interface filter.** Names the source diff introduces — new functions and
  methods, new classes, new module-level constants, new parameters on existing
  signatures — must not appear in the added lines of the test diff. A fix graded
  through a name its author invented would fail a correct solution that chose
  a different name; the canary would then measure naming luck. This is the
  filter that makes the grade fair, and it rejected seven candidates that had
  passed every other check on the first run.
- **Installs in under four minutes** by the same ladder `bench/run.sh` climbs:
  `.[dev]`, `.[test]`, `.[tests]`, `.`, `requirements*.txt`, then bare
  `pytest`. The rung that worked is recorded, because a run must build the same
  environment the pick was validated in.

## What is validated offline, per surviving candidate

In a full scratch clone at base, with a venv built by the ladder:

1. **Pre-existing tests are green at base.** The pull request's test files that
   already existed at base are run unchanged and must pass; otherwise a red
   result later could not be blamed on the missing fix. If every test file is
   new in the pull request the check is skipped and the entry says so.
2. **Fail-to-pass exists.** The fixed tests are checked out onto base and run;
   at least one must fail or error.
3. **Gold is green.** The whole pull request is checked out and the same tests
   must pass fully.
4. **The full suite at base is measured, not asserted**, with a ten-minute cap.
   A suite with unrelated failures is still a usable task, but a run must know
   the baseline to read its own result. If the cap hits, the entry carries a
   note instead of a number.

A candidate that fails any of 1 to 3 is dropped with the reason printed as one
line, `reject owner/repo#n: <reason>`, and never admitted.

## The prompt

The prompt is stored verbatim in the pool entry, in the shape `bench/run.sh`'s
`issue_prompt` produces: `Implement issue #<n>: <title>`, a blank line, the body
as GitHub returned it, a blank line, and the sentence *Work in this repository.
Implement the change and make the existing test suite pass. Do not weaken or
delete tests to make them pass.* Storing it means a run never needs GitHub to be
reachable, and never drifts if someone edits the issue later.

## The anchors

Anchors are frozen: `pick.py --anchors` refuses to run when `pool.json` already
has any. A comparison across weeks is only a comparison if the weeks measured
the same thing. The three below were admitted by the run described at the end
of this file; every number was measured by that run, and the exact counts are
in `pool.json`.

ANCHORS_TABLE

## Reproduction

```sh
CANARY_SCRATCH=/tmp/canary-picks python3 bench/canary/pick.py --anchors 3 --per-label 100 --seed 0
```

The search is over live GitHub data sorted by recent activity, so the candidate
list, and therefore which three survive, moves with time; the seed fixes the
walk order over whatever the search returns on the day. `pool.json` records the
seed and the `picked_at` of each anchor. To see the funnel without cloning:

```sh
python3 bench/canary/pick.py --dry-run --per-label 100 --seed 0
```

Fresh picks for one run, skipping every repository already in the pool:

```sh
python3 bench/canary/pick.py --fresh 2 --exclude bench/canary/pool.json --out fresh.json
```
