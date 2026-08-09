# Headless — driving aforge without a person

This is the contract a script, a CI job, or a benchmark harness programs
against. Everything here is a promise about observable behaviour: flags, exit
codes, stream discipline, and the shape of the JSON. Where this file and
`internal/manual` disagree about what aforge *says*, the manual wins; where
this file and the binary disagree about what it *does*, the binary wins and
this file is a bug.

The audience is deliberately two: a harness needs the exit codes and the JSON
schema, and a person needs to know which command actually thinks.

---

## 1. `aforge do` — one errand, the whole living brain

```
aforge do "<task>" [-w dir] [-db path] [-keep] [-timeout N]
                   [--json] [--yes-spend] [--model slug] [--plan-model slug]
                   [--subharness name]
```

**Use this one.** `do` is the resident's own brain with the conversation
removed — the same compile, the same contracts, the same delivery gate, the
same just-in-time repair when a cited gap earns another round, the same replan
when a leaf runs out of room.

It is *not* `plan` + `run`. That pair compiles a graph once, writes it to a
file, and executes exactly what the file says. Everything aforge learned about
doing jobs happens **after** the plan is written, and a frozen graph cannot do
any of it. Use `plan`/`run` to read or hand-edit a plan; use `do` to get work
done.

### Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-w dir` | the current directory | The directory it works in, **edited in place**. Not an output folder — it opens what is there and leaves nothing behind that you did not ask for. |
| `-db path` | a private temp store, deleted on exit | Work in this durable store instead. This is how state survives across runs. |
| `-keep` | off | Keep the private store instead of deleting it; the path is printed to stderr. |
| `-timeout N` | `900` | Hard wall in seconds. A wall, not a schedule — the length of rope at which a wedged run is more useful dead. |
| `--json` | off | Print one machine-readable object instead of the prose deliverable. |
| `--yes-spend` | off | Approve a plan whose price crosses the consent threshold. Equivalent to `AFORGE_PREAUTHORIZE_SPEND=1`. |
| `--model slug` | `AFORGE_MODEL` | The work model for this run. |
| `--plan-model slug` | `AFORGE_PLAN_MODEL` | Model that plans, replans, writes contracts, and runs the delivery gate, when it should differ from the model executing leaves. |
| `--subharness name` | the compiler chooses per node | Force every leaf onto one worker. This build has **`swe`** — a whole software-engineering pipeline that takes a coding issue in a git repository whole: it plans internally, edits in parallel worktrees, judges each change before merging, and audits the result against that repository's own build and tests. It exists for measuring one worker against another; an unknown name is a note on stderr and the default worker, never a refusal. `aforge run` takes the same flag. |

Flags may appear after the task text; `do` reorders its own arguments.

A `swe` leaf is an ordinary node in every way that matters headlessly: it
reports one `exec.Outcome`, it obeys pause and cancel, its cost lands in
`--json`'s usage, its milestones land in `learned[]`, and the whole of the
engine's event stream is written to `.obs/<node>.trace.log` in the workspace.
Its exit codes are the ordinary ones — nothing about the verdict table below
changes when a specialist ran the leaf. See `docs/SUBHARNESSES.md`.

### Exit codes — the verdict

| Code | Name | Means |
| --- | --- | --- |
| `0` | success | The errand settled and the work stands. |
| `1` | failed | It did not work — including *nothing was attempted*. |
| `2` | timeout | The wall arrived first. Partial work exists and is reported. |

**The exit code is the verdict; `settled` is not.** `settled` says only that
nothing this process is waiting for can still move. The two disagree in exactly
one honest way: an errand stopped by a question is **over** (`settled: true`)
and **did nothing** (`exit 1`). A caller that reads `deliverable` and ignores
the exit code will record an interactive question as the answer to the task —
this happened, and `blocked_on` exists so it cannot happen again.

| `settled` | exit | Situation |
| --- | --- | --- |
| `true` | `0` | Worked. |
| `true` | `1` | Refused or asked back — `blocked_on` carries the question, `deliverable` is empty. |
| `false` | `1` | The price crossed the threshold and was not approved; nothing was bought. |
| `false` | `2` | Hit the wall. Partial work; `blocked_on` is set if a question was standing behind the wall. |

### The `--json` object

```json
{
  "deliverable": "the answer, in full — never a receipt, never a pointer",
  "artifacts": ["/abs/path/to/any/file/it/made"],
  "spend": 0.0731,
  "nodes": 6,
  "seconds": 184.2,
  "settled": true,
  "blocked_on": "the question it could not answer, verbatim",
  "learned": ["what one worker told the others mid-flight"]
}
```

| Field | Contract |
| --- | --- |
| `deliverable` | The final state of the work, whole and to its last byte. Never a plan, a pointer, or a progress receipt. Empty when `blocked_on` is set. |
| `artifacts` | Absolute paths to files the run produced. |
| `spend` | Dollars **this run** cost — measured as the delta of today's spend across the run, not a per-call estimate. |
| `nodes` | How many graph nodes the errand came to. A structural read of how large the work turned out to be. |
| `seconds` | Wall clock. |
| `settled` | Nothing pending can still move. See the matrix above — this is not a verdict. |
| `blocked_on` | Omitted unless the run was stopped by a question. Non-empty **only** alongside a non-zero exit and an empty deliverable. |
| `learned` | The job's blackboard: discoveries, pitfalls, a sibling's failure and why. On an ephemeral store this is the only piece of what the run understood that would otherwise die with it — capture it if you care about the run's reasoning. |

### Stream discipline

**stdout is the result and nothing else** — the JSON object under `--json`, the
deliverable otherwise. Everything else goes to **stderr**: the kept-store path,
the "another aforge is resident" notice, the price refusal, and the quiet line.

The quiet line is a structural read of the graph (no model call, one line)
emitted after 30 seconds of silence, because a wedged run and a run thinking
hard look identical from outside. Redirect stderr if you want it; do not parse
stdout around it, because it is never there.

### The store, and how state survives

With no `-db`, each run gets a private store in a temp directory that is
**deleted on the way out**. Isolation is the point of a one-shot: a task run
this way must not inherit half a conversation's assumptions.

Pass `-db path` to keep the graph. Two runs sharing one `-db` share the task
graph, the notebook, and the job blackboard — the second run knows what the
first learned. That is the seam an experiment about memory across tasks is
measured at.

### Another aforge may already be resident

`do` takes the resident lease. If a live resident already holds it, this is
**not** a failure and not a fight: the command is already in the journal, the
resident applies it on its next pass with its own head attached, and this
process becomes what a second chat window is — something watching the same
journal for the result. It says so on stderr and still reports the outcome.

For guaranteed isolation from your own resident, give the run its own state
root with `AFORGE_HOME`.

### Spending consent

A plan whose estimated price crosses the threshold stops and asks. Headless
there is nobody to ask, so the run ends `settled: false`, `exit 1`, with the
estimate on stderr and nothing bought. Pass `--yes-spend` (or
`AFORGE_PREAUTHORIZE_SPEND=1`) to pre-approve. The daily dollar rail is
`AFORGE_DAILY_BUDGET` (`0` = unlimited) and applies regardless.

---

## 2. Recipes

**One errand, machine-readable, isolated:**

```bash
aforge do "fix the failing test in ./pkg/parse" \
  -w "$PWD" --json --yes-spend -timeout 900
```

**A sequence of tasks that must remember each other** — the store is what
carries across them; the working directory carries whatever the tasks did to
it:

```bash
for task in "$@"; do
  aforge do "$task" -db "$RUN/store/graph.db" -w "$RUN/repo" \
    --json --yes-spend -timeout 900 >> "$RUN/results.jsonl"
done
```

**A disposable brain that touches nothing of yours:**

```bash
AFORGE_HOME="$(mktemp -d)" AFORGE_DAILY_BUDGET=5 \
  aforge do "$TASK" -w "$REPO" --json --yes-spend
```

**Reading the verdict correctly:**

```bash
aforge do "$TASK" --json --yes-spend > out.json
case $? in
  0) jq -r .deliverable out.json ;;
  2) echo "wall hit; partial:"; jq -r .deliverable out.json ;;
  *) jq -r '.blocked_on // "failed"' out.json ;;   # never .deliverable here
esac
```

---

## 3. The other headless commands

| Command | What it is for |
| --- | --- |
| `aforge plan "<goal>" [-o graph.json] [--json] [--brief] [--ensemble N]` | Compile a goal to a graph file. For reading and editing a plan by hand. |
| `aforge run <graph.json> [-w dir] [-j 8] [-o done.json] [--yes-spend] [--subharness name]` | Execute exactly what the file says. Byte-stable, no mid-flight thinking. |
| `aforge revise <graph.json> "<what happened>" [--done 1,2,3]` | Re-plan a graph from what actually happened. |
| `aforge show <graph.json>` | Print a graph. |
| `aforge wake [--max-seconds N]` | One full resident pass — evaluate sentinels, fire what is due, journal it, exit. What the standing watch timer runs. |
| `aforge doctor` | Five rows: brain and size, who is resident, watch state, today's spend against the rail, active goals and pending questions. |
| `aforge competence` / `aforge why self` | The measured competence map; today's self-spend receipts. |
| `aforge notebook [retract\|restore <seq>]` | Inspect, search, and retract beliefs. |
| `aforge services [stop <name>]` | Long-running processes it was asked to keep. |
| `aforge models` | The router ledger — ratings and how many observations back each. |
| `aforge rebuild [--yes]` | Discard every derived table and replay the journal. |

---

## 4. Environment

The full list is `aforge --help`. What matters headless:

| Variable | Default | Why a harness cares |
| --- | --- | --- |
| `OPENROUTER_API_KEY` | — | Required. |
| `AFORGE_MODEL` | see `--help` | The work model. `--model` overrides per run. |
| `AFORGE_PLAN_MODEL` | unset | Plans, replans, contracts, and the gate on a stronger model while a smaller one executes leaves. Unset means the work model plans too. |
| `AFORGE_MODELS` | unset | A panel instead of one model: calls cascade cheapest-first and escalate when a verifier catches a failure. Comma-separated slugs or a JSON path. **Changes what a run costs and how it fails — pin it when measuring.** |
| `AFORGE_DAILY_BUDGET` | `20.0` | Daily dollar rail; `0` is unlimited. A run that hits the rail stops. |
| `AFORGE_PREAUTHORIZE_SPEND` | unset | `1` is `--yes-spend` for every run. |
| `AFORGE_HOME` | `~/.aforge` | The whole state root — journal, workspace, CAS, craft, profiles, catalog, skills. One word moves everything; this is the isolation seam. |
| `AFORGE_PROFILE_DIR` | `AFORGE_HOME` | Where measured behaviour is kept. |
| `AFORGE_MAX_DEPTH` | `2` | Levels of decomposition. |
| `AFORGE_NODE_BUDGET` | `60` | Hard ceiling on total nodes. |
| `AFORGE_REASONING` | `off` | Planning-call reasoning effort. |
| `AFORGE_EXEC_REASONING` | model default | Executor-call reasoning effort. |
| `AFORGE_SWE_MAX_COST` | `10.0` | Dollar ceiling on one `swe` leaf's run inside the coding pipeline. Crossing it ends the leaf as a budget stop with a resume checkpoint on disk, not as a failure. |

A variable set in the environment always wins over the `/settings` sheet, and
that row reads read-only in the sheet rather than fighting your shell.

---

## 5. Measuring aforge with this surface

Rules that came from getting them wrong:

- **Read the exit code, never `deliverable` alone.** An empty deliverable with
  `blocked_on` set is a task that was never attempted; scoring it as a wrong
  answer overstates capability failure and hides an unanswered question.
- **Report `seconds` and `spend` from the JSON**, not from your own wall clock
  around the process — they are measured inside the run, and `spend` is a real
  ledger delta.
- **Do not change host, model, or `AFORGE_MODELS` mid-campaign.** Wall clock and
  cost are reported columns; changing what produces them mid-run corrupts the
  comparison rather than improving it.
- **`-timeout` is part of the result.** A cell that hit the wall measured the
  wall as much as the work. Report the timeout rate beside the score or the
  score is not what it appears to be.
- **One `-db` per experimental unit.** Sharing a store across units that were
  meant to be independent leaks learning between them; giving each unit a fresh
  store when the experiment is *about* memory erases the effect being measured.
