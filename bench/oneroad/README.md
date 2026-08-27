# The one-road benchmark

What this measures: whether chat, given a real issue, **chooses the right road
by itself** — answers in words when words are the work, starts a task when the
work is a task, and divides that task only when the material is genuinely wide
— and what that choice is worth in wall clock, cost and quality against the
same issue handed to a single-agent harness.

The corpus is real work only: tasks from the Senior SWE-Bench v2026.06 dataset
(`~/src/senior-swe-bench-v2026.06`, each with a Docker environment, an oracle
patch and a verifier) and live GitHub issues fetched with `gh`, which is the
protocol `bench/README.md` already describes.

Everything below exists so that a number produced today can be compared to a
number produced next month. Read `bench/README.md` first; this file states only
what is different here.

## Every cell is driven through chat

The aforge arm is driven **through a real TUI over tmux**, never `aforge do`.
The question is what the chat surface decides, and a headless entry point
answers a different question — it is handed the shape instead of choosing it.
`bench/e2e/` already drives the TUI this way and its lib is what these cells
use.

`pi` and `opencode` get the same issue text through their own front doors. They
have no task concept and no road to choose, which is exactly why they are the
reference point.

## What every cell records

Deterministic, from the run itself — never from the harness's own account of
how it did:

| column | where it comes from |
|---|---|
| `wall_s` | the cell's own clock |
| `cost_usd` | the journal's `usage` table, summed. Never estimated |
| `tests_before` / `tests_after` | the repository's suite, run before and after |
| `changed_files` | `git status --porcelain` |
| `road` | `words`, `task`, or `task+divided` — what chat actually did |
| `armed` | whether the task was armed to divide |
| `parts` | how many parts it divided into, 0 if none |
| `peak_workers` | most workers alive at once. This is what parallelism IS |
| `refused` | division requested and refused, with the gate that refused it |

The last five are the point of this benchmark. A row where the road was right
but slow tells us something a row where it was fast and wrong does not.

## The judge

Scores that a test suite cannot produce come from a **Sonnet judge**, run as a
subagent per cell, against the pinned prompt in `JUDGE.md`.

Repeatability rules, all of them load-bearing:

1. **The prompt is a file with a version, and it is never edited in place.**
   Every judgement records `judge_prompt` (the filename) and `judge_sha`
   (sha256 of that file, first 16 hex). A row whose sha does not match the
   current file was scored against different words and is not comparable —
   the recorder refuses to mix them silently.
   `JUDGE.md` = version 1 = `455fc14cdf00ba53`.
2. **The judge is blind.** It is told the issue, the diff, the test counts and
   the attempt's own report. It is never told which harness produced them, and
   it never sees the oracle patch — a judge that has the answer grades
   similarity to the answer instead of the work.
3. **Three judgements per cell, independent.** The score is the MEDIAN of the
   three and the spread is recorded. A dimension whose spread is 2 or more is
   reported as unreliable rather than averaged into a clean-looking number.
4. **Model pinned**: `claude-sonnet-5`. Recorded per row as `judge_model`.
   A different model is a different instrument; it gets a new column value,
   not a quiet substitution.
5. **The deterministic verdict always wins.** Tests, changed files and the
   dataset's own verifier are facts; the judge grades what is left. A cell
   whose suite regressed is a failure whatever the judge said about it.

`weakened_tests` is a hard gate, not a score: a cell that weakened a test to go
green is reported as a failure with its other numbers shown but struck through,
because the fastest way to a green suite is always to delete the test.

## Reading a comparison honestly

- Quote wall clock only beside the verdict. A harness that finishes in half the
  time having done half the work is not faster.
- `n=1` rows are directional. Say so wherever they are quoted.
- The traps in the corpus are as important as the wide rows: a system that
  divides a deep, serial bug fix has failed that row even if it lands the fix.
