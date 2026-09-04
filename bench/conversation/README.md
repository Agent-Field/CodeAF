# Conversation battery

Cross-harness comparison of aforge against omp, pi and opencode on ordinary
work **and** on conversation — the parts of using a coding agent that a
`--print` invocation cannot reach.

`run.sh` runs it, `summary.sh` reads it, `test/selftest.sh` checks the rig
itself without spending anything. This file says what each number means and
which ones it is honest to quote.

**It spends real money and is on demand.** Nothing in `make check` reaches it.
The deterministic tests under `test/` are the part that is safe to run any time:
they use fake binaries, call no model, and touch no network.

## The two doors, which are never mixed

| door | what it is | what it can show |
|---|---|---|
| `print` | one message in, one reply out (`aforge chat --once`, `omp -p`, `pi -p`, `opencode run`) | quality, cost and wall clock on a fixed task |
| `interactive` | the real TUI in a tmux pane: bracketed paste, Enter, and a screen that is watched | everything above, plus what happens when a person types **while work is running** |

A print row is never labelled interactive. An arm with no interactive door this
suite can drive is recorded `unsupported` — not quietly run through the print
door instead, which is the one substitution that would make the whole table a
lie.

An arm has an interactive door here only if this suite can tell from the screen
alone when it is working and when it is not. Those markers, and where each came
from, are in `lib/adapters.sh`:

| arm | interactive door | markers from |
|---|---|---|
| aforge | yes | `bench/canary/lib/chat.sh`, which drives this TUI on a schedule in this repository |
| pi | yes | live pane capture on pi 0.84.2 in this lane |
| omp | yes | live pane capture on omp 18.1.2 in this lane |
| opencode | **no** | no calibrated markers — its interactive cells are `unsupported` |

## Open models only

Every call this suite causes goes to the pinned open model. The default pin is
`deepseek/deepseek-v4-flash-0731`, an exact catalog id rather than a floating
alias. It is configurable (`--model`, `--allowlist`), and the protocol assumes
no particular id — but the shape is fixed: exact ids, no wildcards, no family
names, and `:batch`-style variants count as different entries because they are
a different queue with a different price and latency.

Three gates, because none of them is sufficient alone:

1. **Environment.** A harness process inherits `PATH`, `HOME`, `TERM`, `LANG`,
   `TMPDIR` and `OPENROUTER_API_KEY`, and nothing else (`lib/common.sh`,
   `CONV_CARRIED`). A peer that never sees an Anthropic or OpenAI key cannot
   bill one. `CONV_PASS_ENV` names anything extra to carry, so what got through
   is always visible in the cell's `config.txt`.
2. **Before the call.** The arm is handed the exact id; its own catalog is
   asked whether that id exists; and an arm that cannot pin its *auxiliary*
   calls — titles, planners, "smol" helper roles — is **skipped rather than
   run**. aforge pins them with `--one-model`, omp with `--smol/--slow/--plan`.
   pi 0.84.2 and opencode document no such pin, so they are skipped by default;
   `--role-pin off` runs them anyway and says so on every row.

   The catalog is asked in the state root the cell will use, not the
   operator's, because they answer differently: on the machine this was built
   on, pi's default profile lists no openrouter models at all while a fresh
   `PI_CODING_AGENT_DIR` does. The query pattern is loose and the match is
   exact — provider **and** id for pi, the full selector for omp, since another
   provider's row carrying the same id is not evidence that this arm can pin
   it. aforge has no offline catalog query, so its pin is enforced on receipts
   only, which is the stronger check anyway: it sees the roles.
3. **After the call.** Every model id in the cell's own receipts is checked
   against the allowlist. This is what catches a role or a fallback that
   resolved elsewhere, and a cell that billed one fails.

## Cost, and what unknown means

Cost is self-reported usage only, read from each harness's own receipts:

| arm | receipt | names the billed model? |
|---|---|---|
| aforge | `AFORGE_HOME/v3/usage.jsonl`, one row per call including auxiliary roles | yes |
| pi, omp | the JSON Lines events on stdout under `--mode json` | yes |
| opencode | `step_finish` events under `--format json` | **no** |

A harness that reported no usage gets `cost_usd: null` and
`cost_source: "none"` — never `0`. Zero is a measurement; null is the absence of
one, and a frontier that reads an absence as a zero puts the quietest harness on
top. Cells without a cost, and cells whose billed model cannot be named, are
marked `comparable: no` and are excluded from every comparison with their
reason attached.

Known gap: through the interactive door, pi and omp stream nothing to stdout, so
their receipts are read opportunistically from the session files in their
session directory. That schema has not been verified here; when it does not
parse, the cell comes back `cost unknown` and not-comparable rather than zero.

## Effort

Asked of every arm as one rung (`--effort`, default `low` — the only rung
aforge, omp and pi all have; pi 0.84.2 has no `medium`). An arm without the rung
runs and is marked **not comparable**, with the mismatch on the row. Nothing is
silently moved to a neighbouring rung: a row that ran a rung above the others is
the most flattering possible lie about cost.

opencode's `--variant` is provider-specific and enumerates nothing, so its
effort is recorded as `unverified` and its rows are not comparable on that
basis.

## The scenarios

| scenario | workload | door | what it checks |
|---|---|---|---|
| `data-tally` | data | print | exact arithmetic over a handed CSV, against an answer key computed from the same file |
| `research-brief` | research | print | facts joined across four notes, one of which is superseded and contradicts the current one |
| `writing-memo` | writing | print | a deliverable on disk under countable constraints: word ceiling, three required facts, one banned word, a required closing line |
| `code-fix` | coding | print | a real boundary bug in a Go module: the module's own suite is the judge, and the test file is checksummed so "made the tests agree" fails |
| `followup-while-working` | conversation | interactive | a second question typed **while** a slow job runs: it must be answered, and the job must still finish |
| `revision-midwork` | conversation | interactive | the deliverable's shape is changed mid-flight: the revised file must exist, correct, and the superseded one must be gone |
| `task-result-delivered` | conversation | interactive, aforge only | work is handed off, and afterwards the person asks what it produced: the number on the screen must be the number really in the file |

Fixtures are deterministic and offline (`fixtures/`). The interactive ones use a
script that sleeps, so the busy window costs a sleep rather than tokens.

`task-result-delivered` exists for aforge alone because it is about aforge's own
surface; on any other arm it is recorded `unsupported`. **It asserts nothing
about shape** — not how many agents ran, not whether a task was spawned. A build
that answers in one turn with no task at all passes it, and should: the person
asked for a result, not an org chart.

## Outcomes

`pass`, `fail`, `timeout`, `crash`, `skipped`, `unsupported` are six different
words and only the first is a success. The summary counts each separately and
the runner's exit code moves for `fail`, `timeout` and `crash` only. A skipped
cell that quietly read as a pass is the failure this whole suite is built to
prevent.

The interactive door adds one more: if the harness never went visibly busy,
`no-busy-window` is recorded and the cell **fails** — the scenario did not
happen, so there is nothing to pass.

## Reading a run

```sh
bench/conversation/summary.sh                 # the newest run
bench/conversation/summary.sh <results.jsonl> # one or more runs
```

It prints, **per workload**, each arm's mean quality (the fraction of that
cell's own assertions that passed), mean cost and mean wall clock, and then the
arms nothing else beats on all three at once. It does not add the workloads
together: one arm being cheaper and another being better is the normal result,
and a single number across coding, research, writing and conversation would be
an average over incomparable things. When one arm is on the frontier everywhere,
the tool says so *and* says how few workloads, rungs and samples that is.

## Evidence

Per run under `bench-results/conversation/<run-id>/`, per cell:

```
config.txt      binary, version, pins, allowlist, effort, isolation, cap, paths
argv.txt        the exact invocation
prompt.txt      the message every arm was given, byte for byte
stdout.log      what the harness streamed
receipt.json    normalised cost, tokens, models, turns
reply.txt       the reply, extracted from whichever shape it arrived in
scrollback.txt  the whole conversation (interactive door)
door.json       what the driver observed: ended, turns sent, busy seen, markers
results.jsonl   one row per cell, with every assertion and its outcome
```

`config.txt` records the *names* of credentials present, never a value, and is a
curated list rather than an environment dump: a redaction regex over everything
a shell happens to hold is one unfamiliar variable name away from publishing a
secret.

Evidence is never overwritten by accident — a run whose cell directory already
exists refuses and records a skip. `--overwrite` is the deliberate way.

State is isolated per cell: `AFORGE_HOME` for aforge, `PI_CODING_AGENT_DIR` plus
`--session-dir` for pi, `XDG_*` for opencode (declared, not documented by
opencode, and recorded as unverified). omp's documented isolation is a named
profile under `$HOME/.omp/profiles`; this suite creates its own per-cell profile
(and seeds `setupVersion: 2`, without which a fresh profile opens a setup wizard
and the TUI never reaches a composer) and removes **only** a profile it created.
An existing profile named with `CONV_OMP_PROFILE` is used as-is and never
deleted, and a name that is not a plain path component is refused.

## Testing the rig

```sh
bench/conversation/test/selftest.sh      # deterministic, offline, free
bench/conversation/run.sh --dry-run      # compose every invocation, run none
```

The selftest runs the whole battery against fake binaries whose misbehaviour is
known, and fails if the rig does not catch it: a dropped exit code, a fluent
wrong answer, a harness that hangs, usage that was never reported, a model off
the allowlist (including one reached only by an auxiliary role), a catalog that
cannot pin the id, a scenario an arm has no door for, a followup that never
arrives, a busy window that never happens, an existing omp profile, a second run
that would overwrite the first's evidence, and credentials leaking into a child
process. It also checks the receipt reader counts a repeated message once — all
three peers emit the same assistant message three times.

At the time of writing it is 50 checks, all passing, and it needs `tmux`; a
missing dependency is reported as skipped and exits non-zero rather than green.

## What this does not establish

One machine, one model, one effort rung, one attempt per cell. Quality is the
fraction of a cell's own assertions that passed, which measures what the cell
checks and nothing else — the writing cell counts words and facts, not whether
the memo is any good. The conversation cells use a sleeping script as the slow
job, so they measure whether a followup lands and the work survives, not how a
harness behaves under a genuinely expensive one. Wall clock carries provider
latency and queueing; two runs of identical code have come in 60% apart on the
older batteries in `bench/`.
