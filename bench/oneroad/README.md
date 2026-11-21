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

The codeaf arm is driven **through a real TUI over tmux**, never `codeaf do`.
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

## Measuring the script tool

`codeaf-script-flash` and `codeaf-script-crew` are `codeaf-new-flash` and
`codeaf-new-crew` with `CODEAF_TASK_SCRIPT=on` in codeaf's environment and
nothing else changed, so each pair measures the task worker's `script` tool
alone (internal/session/tool_script.go). Run the pairs on the same tasks and
seeds:

```sh
bench/oneroad/wave.sh <task> codeaf-new-crew codeaf-script-crew
# for example: NEW_BIN=$PWD/bin/codeaf bench/oneroad/wave.sh 21 codeaf-new-crew codeaf-script-crew
```

Every codeaf cell's `meta.json` carries, and `collect.py` puts in
`results.csv`:

- `steps`, `script_steps`, `script_calls`: the workers' tool calls, their
  script calls and the calls those scripts made, read from plan-run
  trajectories and chat-task transcripts alike (`lib/script_use.py`). Read
  these first: a script arm whose workers wrote no scripts measured nothing
  about the tool, and its differences are noise.
- `worker_usd`, `worker_tokens_in`, `worker_tokens_out`,
  `worker_tokens_cache`, `worker_model_calls`: every model call a task worker
  made, from the profile's usage ledger (`v3/usage.jsonl`, the rows on the
  worker seat or naming a task: a plan run's workers carry only the seat). This is where the tool's effect on spend shows; compare it between
  the pair. The plan store's spend rows are not used: they hold only a plan
  run's workers, and a chat task's spend is not in them.
- `tokens_in`, `tokens_out`, `tokens_cache`: the conversation's own model
  calls (`lib/road.py`), the same names the peer arms' tokens use. They do
  not include the workers.

`cost_usd` prefers the conversation's call lines when it has them, so it can
leave the workers' bill out; for this comparison read `worker_usd` beside it.
Then compare outcome, tests after and `wall_s_active`. A run calls live models
on the key in `OPENROUTER_API_KEY` and is billed.

### What the pilots found

Four pilot cells on `batch` (2026-11-18) wrote one script between them, and
it failed: DeepSeek v4 flash read the tool as Node.js, and Sonnet 5.5 did its
reading in `bash` instead, chaining commands in one call. A grid here would
measure that the tool goes unused. `bench/scriptbench` measures the case it
is for, and found the saving there.

### The chat-side arm

`codeaf-chatscript-flash` and `codeaf-chatscript-crew` are the new arms with
`CODEAF_CHAT_SCRIPT=on`: the same tool on the conversation's own belt, the
workers untouched. Run them beside `codeaf-new-*` in the same wave:

```sh
NEW_BIN=... bench/oneroad/wave.sh batch codeaf-new-crew codeaf-chatscript-crew
```

Read `chat_script_steps` and `chat_script_calls` first, for the reason the
worker arm's counts come first. The effect shows in the conversation's own
`tokens_in`, `tokens_out` and `tokens_cache`, beside `worker_*` and `road`:
a chat that can make many calls in one step may do work itself that it would
otherwise have handed to a task, and that is a change of road, not a saving.

## Reading a comparison honestly

- Quote wall clock only beside the verdict. A harness that finishes in half the
  time having done half the work is not faster.
- `n=1` rows are directional. Say so wherever they are quoted.
- The traps in the corpus are as important as the wide rows: a system that
  divides a deep, serial bug fix has failed that row even if it lands the fix.
