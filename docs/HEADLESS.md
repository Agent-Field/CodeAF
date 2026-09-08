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
aforge do "<task>" [--dir dir] [--db path] [--keep] [--timeout D]
                   [--json] [--yes-spend] [--model slug] [--plan-model slug]
                   [--context-fill N] [--completion-reserve N]
```

**Use this one.** `do` is the resident's own brain with the conversation
removed — the same compile, the same contracts, the same delivery gate, the
same just-in-time repair when a cited gap earns another round, the same replan
when a leaf runs out of room.

It is *not* `plan new` + `plan run`. That pair compiles a plan once, writes it
to a file, and executes exactly what the file says. Everything aforge learned
about doing jobs happens **after** the plan is written, and a frozen plan cannot
do any of it. Use `aforge plan` to read or hand-edit a plan; use `do` to get
work done.

### Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--dir dir` | the current directory | The directory it works in, **edited in place**. Not an output folder — it opens what is there and leaves nothing behind that you did not ask for. `-w` is the shorthand and keeps working forever. |
| `--db path` | a private temp store, deleted on exit | Work in this durable store instead. This is how state survives across runs. |
| `--keep` | off | Keep the private store instead of deleting it; the path is printed to stderr. |
| `--timeout D` | `15m` | Hard wall, as a duration with a unit: `5m`, `2h`, `90s`. A bare number is still read as seconds for one release, so `--timeout 900` keeps working. A wall, not a schedule — the length of rope at which a wedged run is more useful dead. |
| `--json` | off | Print one machine-readable object instead of the prose deliverable. |
| `--yes-spend` | off | Spend past today's limit and past the plan-price question, without stopping to ask. The same flag with the same one sentence on `aforge plan run`. Equivalent to `AFORGE_PREAUTHORIZE_SPEND=1`. |
| `--model slug` | the ladder below | The work model for this run. |
| `--plan-model slug` | the ladder below | Model that plans, replans, writes contracts, and runs the delivery gate, when it should differ from the model executing leaves. |
| `--context-fill N` | `60` | How full a model's context window may get before it is compacted, in percent; the law clamps it to 10–90. Setting it is what makes it govern a conversation's fold line as well — unset, that line follows the model's window. |
| `--completion-reserve N` | `65536` | Tokens every call keeps free for its visible answer *and its reasoning*. Raise it for a reasoning-heavy model that truncates; lower it to buy prompt room on a small window. |

A run that ends without delivering and without writing anything says so in its own answer:
`No created or changed files were recorded: this run worked in /srv/project, editing it in place.`
The run's own traces live under the kept store named by the `record kept at <path>` line on stderr; that store is a different place from the working directory.

Flags may appear after the task text; `do` reorders its own arguments. Naming
neither context flag touches the environment at all, so a wrapper script that
exported `AFORGE_CONTEXT_FILL_PCT` for a whole campaign stays in charge of it.

### The brief may begin with anything, including `-`

**A flag is a flag because this command declares one by that name.** Everything
else is the brief, whatever it starts with — so a task written as a bullet list
is a task:

```sh
aforge do "- Update the display style property
- Keep the grid measurable"
```

That used to die in one second with `flag provided but not defined: - Update the
display style property…` and a usage dump, because the reordering above decided
by shape and a leading dash meant a flag. It decides by the flag set now, and a
flag name holds no whitespace, so a bullet, a sentence and a multi-line brief are
all text. A **misspelt** flag is still refused by name — `--dbb /tmp/x` is an
error, not a brief — which is the reason the rule is not "anything with a dash is
text". `--` ends the flags in the usual way, for a brief that really is one word
beginning with a dash.

### The brief may arrive on stdin

Pass `-` as the task, or pass no task at all when something is piped in:

```sh
aforge do - < brief.md
cat brief.md | aforge do --json
generate-brief | aforge do -w /repo -
```

`aforge do` with no task and a terminal attached prints the usage rather than
reading your keyboard forever. Everything after the brief is unchanged: the text
is taken byte for byte, exactly as a quoted argument is.

### The task is the task — verbatim fidelity

**What you pass to `do` becomes the goal, byte for byte.** The compile stage
still runs, and the planner still gets everything it reads from it — how large
the work is, what parts it splits into, which earlier jobs it continues, the
model words, the title. What it does not get is a rewrite: the goal it plans
against is the sentence you submitted, trimmed of surrounding whitespace and
otherwise untouched, and the compiler's speculative assumptions are dropped
rather than anchored into the leaves as decisions the work is held to.

This is the one deliberate difference between `do` and the chat surface. Chat's
value at this seam is precisely that it re-asks the question better — it rewords
a half-formed ask into a goal, declares what it is assuming, and stops to ask
when one of those assumptions is too consequential to guess. A caller
programming against `do` already wrote the specification, and a compiler that
improved it would mean the harness measured something nobody wrote, with the
reworded goal as the only version the journal ever kept.

**Questions are assumed, not asked.** Where chat would stop and ask,
`do` takes the answer the compiler itself ranked first, records the skipped ask
in the journal so a later correction can find it, and declares the answer as a
working decision on the goal and on the node the delivery gate judges. A run
that asked into an empty room has done nothing; a run that assumed and said so
has done the work and left the assumption on the record. So `blocked_on` is
rarer than the exit-code table below implies — it is what remains when the
question was not the compiler's to answer at all.

The corollary for a harness: put the answer in the ask. Anything you leave
implicit is something `do` will decide for you and tell you it decided.

### Which models a run uses — one ladder, four rungs

The two seats — the model that **works** and the model that **plans** — resolve
the same way at every headless door (`do`, `exec`, `run`, `plan new`,
`plan revise`, `plan run`). First rung that answers wins, per seat:

| | work seat | plan seat |
| --- | --- | --- |
| 1 | `--model slug` | `--plan-model slug` |
| 2 | `AFORGE_MODEL` | `AFORGE_PLAN_MODEL` |
| 3 | the profile's crew — the **small work** row | the profile's crew — the **mastermind** row |
| 4 | the build's default (`aforge --help`) | empty: the work model plans too |

**Rung 3 is what `/crew` writes** (`models.tiers.*` in the profile's
`config.json`), and it is the rung that used to be missing: until #166 a headless
run read the flags and the environment and never opened the profile, so a machine
told `frugal` in the chat ran something else the moment the same brain ran
headless. The two rows are the ones the chat's own planner and worker ride, so
the crew now means the same thing on both surfaces.

The crew answers only where a crew was actually **written**. A profile nobody has
touched falls to rung 4 — the four shipped tier values are the `balanced` row, so
reading them as a crew would make rung 4 unreachable and change the default work
model for everybody. `AFORGE_HOME` / `AFORGE_PROFILE_DIR` decide which profile is
asked, so an isolated run is isolated here too.

A crew row may carry a thinking level (`moonshotai/kimi-k3:low`), and so may a
flag or a variable. The value travels whole and the level is applied per call by
the role ladder, exactly as it is in the chat; the slug sent to the provider is
the model alone. (Until this landed it was sent whole, so `--plan-model
kimi-k3:low` asked OpenRouter for a model id nobody publishes.)

**Every run says which rung answered**, on stderr, before anything else:

```
models: work deepseek/deepseek-v4-flash (crew frugal) · plan qwen/qwen3.8-27b (crew frugal)
models: work anthropic/whatever (--model) · plan follows the work model (default)
```

so a campaign can verify what actually ran instead of trusting the shell it
launched from. `do --json` carries the same four facts as fields.

### Exit codes — one ladder, and it is the same one on all three commands

| Code | `stop` | Means |
| --- | --- | --- |
| `0` | `done` | It is done, and what is on stdout is the answer. |
| `1` | `error` | It could not be run at all — no key, bad arguments, the store would not open, the workspace could not be made, no resident took it. Nothing was attempted and nothing was spent. |
| `2` | `incomplete`, `unchecked` | It ran and did not finish: part of the work does not stand. The job failed or was cancelled, the delivery did not land whole, or a refusal that was not a question. Whatever it DID manage is on stdout and is worth reading. `unchecked` is the same rung and a different fact: the work was delivered and the final check could not be reached, so nothing has vouched for it — read it, it may be perfectly good. |
| `3` | `budget`, `turn-cap`, `deadline`, `price` | A limit you set stopped it — the token budget (`--token-budget`), the turn cap (`--max-turns`), the wall (`--timeout`), or a plan price that crossed the consent threshold with no `--yes-spend`. The work was going when it was cut off; raise the limit and run it again. |
| `4` | `question` | It stopped to ask and nobody was there. The question is on stderr verbatim and in `blocked_on`. |

**This moved.** `do` used to return 0, 1 and 2 only, where `1` meant everything
from "nothing was attempted" to "it asked a question" to "the price was refused",
and `2` meant both "the delivery did not land whole" and "the wall came first".
Those are five different situations and a script could branch on none of them.
The whole before-and-after, per command, is
`docs/design/polish/envelope-and-exits.md`.

`2`'s causes are one fact: **the deliverable did not land whole.** The delivery
gate — the judge that asks whether the person who asked would accept this —
rejected the deliverable and stood by the rejection; or part of the job failed
or was cancelled, which the deliverable itself says out loud ("Not all of this
landed: 1 of 2 parts finished").

A gate verdict the system overruled is **not** a rejection: a gap the one polish
pass closed, and a gap refused as ungrounded or as already closed, exit `0`.

There is one exception, and it is the difference between an opinion and a fact.
The gate has a **mechanical half**: before a judge is paid anything, every file
the plan's own stopping criterion named must be on disk and non-empty. When one
is not, the gap it raises names those files — one citation per file — and it is
marked mechanical. The admission rules may still refuse to buy a repair round
over it, but refusing a citation cannot make a file appear, so a refused
mechanical gate exits `2`, not `0`. Every other refusal says a judge was wrong
about the text; this one says the plan promised a file that is not there.

**Read `ok` or the exit code; `settled` is neither.** `settled` says only that
nothing this process is waiting for can still move. The two disagree in exactly
one honest way: an errand stopped by a question is **over** (`settled: true`)
and **did nothing** (`ok: false`, exit `4`). A caller that reads `answer` and
ignores the exit code will record an interactive question as the answer to the
task — this happened, and `blocked_on` exists so it cannot happen again.

| `settled` | `ok` | exit | Situation |
| --- | --- | --- | --- |
| `true` | `true` | `0` | Worked, whole. |
| `true` | `false` | `4` | Asked back — `blocked_on` carries the question, `answer` is empty. |
| `true` | `false` | `2` | Refused, or delivered and not whole — the gate rejected it, or parts of it did not land. `answer` says which. |
| `false` | `false` | `3` | The price crossed the threshold and was not approved; nothing was bought. Or the wall arrived. |
| `false` | `false` | `4` | The wall arrived with a question standing behind it. |

### The `--json` object — ONE shape, on `do`, `exec` and `run` alike

```json
{
  "ok": true,
  "stop": "done",
  "answer": "the answer, in full — never a receipt, never a pointer",
  "files": ["/abs/path/to/any/file/it/made"],
  "error": "",
  "spend_usd": 0.0731,
  "tokens": {"in": 18422, "out": 1130},
  "seconds": 184.2,
  "model": "deepseek/deepseek-v4-flash",
  "steps": 6,
  "run": "0123456789abcdef",
  "calls": 47,
  "rounds": 2
}
```

| Field | Contract |
| --- | --- |
| `ok` | The work stands. True on exactly the runs that exit `0`. |
| `stop` | Why it ended, in one word: `done`, `error`, `incomplete`, `unchecked`, `budget`, `turn-cap`, `deadline`, `price`, `question`. **This is the field to read.** The exit code says how much is wrong; `stop` says what. |
| `answer` | The final state of the work, whole and to its last byte. Never a plan, a pointer, or a progress receipt. Empty when `blocked_on` is set. |
| `files` | Absolute paths to files the run produced. Always a list, never `null`. |
| `error` | Why it could not be run at all, in the same words stderr carried. **Always present**, and empty on a run that started — including a run a limit cut short, whose partial answer is in `answer` and whose reason is in `stop` and `incomplete`. |
| `unjudged` | On `aforge do`, why nothing checked the delivery — how the gate was asked and the provider's own sentence. It appears on exactly the runs nothing checked, so its presence is itself the answer to "was this checked?" and a caller never has to read the sentence. |
| `spend_usd` | Dollars **this run** cost — measured as the delta of today's spend across the run, not a per-call estimate. |
| `tokens` | `{"in": …, "out": …}`. |
| `seconds` | Wall clock. |
| `model` | The model the work ran on. |
| `steps` | How many pieces of work ran. A saved program does not count them and reports `0`. |
| `run` | This invocation's id. `--debug` names its folder after it and every row this run wrote into `~/.aforge/logs/calls.jsonl` carries it, so `aforge logs --run <id>` joins the two. |
| `calls` | Model calls this run made, counted whether or not the call log is switched on. |
| `rounds` | How many times the run bought more work after looking at what it had. `exec` and a saved program do not measure it and report `0`. |

**The old field names are still printed, beside the new ones, for one release**,
so nothing that reads them breaks today: `deliverable` → `answer`, `artifacts` →
`files`, `spend` → `spend_usd`, `nodes` → `steps`. `exec`'s `text`, `turns`,
`elapsed_ms` and `usage` are the same story on that command.

**`settled` is not the old name of `ok` and is not deprecated.** It means
"nothing this run is waiting for can still move", which is *true* of a run that
asked a question and did nothing. Reading one as the other records every refusal
as a success.

**The one reader this genuinely broke** is a caller that tested for the
*presence* of `error` to detect failure. It used to be `omitempty`; it is always
there now. Test its value, or read `ok`.

Fields that belong to `do` and stay: `spend_work` and `spend_overhead` — what
the work cost against what it cost to decide what the work should be —
`blocked_on`, `learned`, `plan_model`, `model_source`, `plan_model_source`,
`subharness` and `workspace`.

| Field | Contract |
| --- | --- |
| `blocked_on` | The question it could not answer, verbatim. Non-empty **only** alongside a non-zero exit and an empty `answer`. |
| `learned` | The job's blackboard: discoveries, pitfalls, a sibling's failure and why. On an ephemeral store this is the only piece of what the run understood that would otherwise die with it — capture it if you care about the run's reasoning. |
| `model_source` / `plan_model_source` | Which rung of the ladder above chose each: `--model`, `AFORGE_MODEL`, `crew frugal`, `default`. Pin these in a campaign's records — they are the only way to tell two cells apart that were launched from different profiles. |
| `workspace` | The directory the run worked in, absolute, edited in place. Always present; empty only on a run that never got as far as opening one. |

### Stream discipline

**stdout is the result and nothing else** — the JSON object under `--json`, the
deliverable otherwise. Everything else goes to **stderr**: the `models:` line the
run opens with, the kept-store path, the "another aforge is resident" notice, the
price refusal, and the quiet line.

The quiet line is a structural read of the plan (no model call, one line)
emitted after 30 seconds of silence, because a wedged run and a run thinking
hard look identical from outside. Redirect stderr if you want it; do not parse
stdout around it, because it is never there.

A **fault line** appears on stderr when something interrupted a leaf and the run
carried on anyway — a recovered panic, and now a provider call the guard cut:

```
  ✗ Core engine                  — nothing came back from the model in 1m30s → the call was retried, routed away from deepinfra  4m12s
```

Read `the call was retried` literally. What is asked again is the CALL, with
everything the leaf had already done still in hand; the leaf is not restarted and
its work is not thrown away. A stalled endpoint also loses its lane for five
minutes, so the retry goes somewhere else — which is what `routed away from` says
when the wire named who was serving.

A leaf that IS restarted — because the claim reaper found a claim nobody was
holding — resumes rather than starting over: it is handed its own recorded turns,
what it had already said, and the files it had already written. See PERF.md's
liveness laws for the bounds.

### The store, and how state survives

With no `--db`, each run gets a private store in a temp directory that is
**deleted on the way out**. Isolation is the point of a one-shot: a task run
this way must not inherit half a conversation's assumptions.

Pass `--db path` to keep the store. Two runs sharing one `--db` share the task
plan, the notebook, and the job blackboard — the second run knows what the
first learned. That is the seam an experiment about memory across tasks is
measured at.

### Another aforge may already be resident

`do` takes the resident lease. If a live resident already holds it, this is
**not** a failure and not a fight: the command is already in the journal, the
resident applies it on its next pass with its own head attached, and this
process becomes what a second chat window is — something watching the same
journal for the result. It says so on stderr and still reports the outcome.

One thing does change when that happens, and it is the reason to avoid it in a
measured campaign: the verbatim law above is a fact about the brain that applies
the command, not about the command itself. A resident that picks it up compiles
it the way a conversation would — reworded goal, declared assumptions, and a
question asked into a thread nobody is reading.

For guaranteed isolation from your own resident, give the run its own state
root with `AFORGE_HOME`.

### Spending consent

A plan whose estimated price crosses the threshold stops and asks. Headless
there is nobody to ask, so the run ends `stop: "price"`, `ok: false`, **exit 3**,
with the estimate on stderr and nothing bought. Pass `--yes-spend` (or
`AFORGE_PREAUTHORIZE_SPEND=1`) to pre-approve. The day's spending limit is
`AFORGE_DAILY_BUDGET` (`0` = unlimited) and applies regardless.

---

## 2. `aforge exec` — one linear worker, no plan

```
aforge exec ["<prompt>"] [--dir dir] [--system text]
            [--max-turns N] [--token-budget N] [--timeout D]
            [--model slug]
            [--context-fill N] [--completion-reserve N]
            [--json] [--out file]
```

`exec` runs **one** agent with the tool loop and nothing else: no compile, no
plan, no working methods, no delivery gate, no replan, no journal, no lease.
It is the bottom of the product — the same executor a leaf runs on — exposed
directly.

Reach for it when the caller has already decided what the work is and wants the
cheapest, most predictable path to an answer: a sub-harness embedding aforge in
its own pipeline, a benchmark measuring the raw worker, an agent framework that
does its own planning. Reach for `do` when you want aforge to decide how the
work divides, to repair itself mid-flight, and to judge what it produced. `exec`
does none of that, and the price of the missing machinery is that nothing checks
the answer.

With no prompt argument it reads the prompt from **stdin**, which is how a
harness passes anything with newlines in it.

### Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--dir dir` | `.` | The directory the worker works in, created if missing. Also where its scratch lands — see below. `-w` is the shorthand and keeps working forever. |
| `--system text` | empty | The working method, passed as the task's contract. |
| `--max-turns N` | `200` | Runaway backstop on agent iterations. Hitting it exits `3`. |
| `--token-budget N` | `150000` | Token budget for the whole run. Hitting it exits `3`. |
| `--timeout D` | scaled from `--token-budget` | Hard wall, as a duration: `15m`, `2h`, `90s`. A bare number is read as seconds, so `--timeout 900` keeps working. Unset, it is 15 minutes, or one minute per 50k tokens of budget when that is longer. Hitting it exits `3`. |
| `--model slug` | the ladder in section 1 | The work model. `exec` opens with the same `models:` line on stderr, naming its one seat and the rung that chose it. |
| `--context-fill N` | `60` | How full the context window may get before it is compacted, in percent. |
| `--completion-reserve N` | `65536` | Tokens kept free for the answer and its reasoning. |
| `--json` | off | Print the envelope below instead of the plain text. |
| `--out file` | — | Also write the envelope to this file. Independent of `--json`: the file is always the JSON. `-o` is the shorthand. |

Flags may appear after the prompt text; `exec` reorders its own arguments.

**Three flag names moved, and every old spelling still works for one release.**
`--turns` is `--max-turns`, `--budget` is `--token-budget` — *budget* is a word
about money everywhere else in this product, so `--budget 150000` read as
$150,000 — and `-w`/`-o` became `--dir`/`--out` with the letters kept forever as
shorthands. A renamed spelling prints ONE line on **stderr** the first time it is
used and is absent from `--help`; a shorthand says nothing, because it is not
going away. The notice is never on stdout, so `--json | jq` keeps parsing.

**`--plan-model` is gone from this door.** It was accepted "for headless
model-pin parity" and documented as doing nothing, which teaches a harness author
a wrong thing quietly. It is still parsed, so a script passing it keeps running,
and it now says `note: exec does not plan — --plan-model has no effect here.`

### The walls can come from the environment

The three walls — and only those three — fall back to the environment when the
flag was not passed, so a harness can set them once for a campaign instead of
threading them onto every call:

| Variable | Flag it stands in for | Units |
| --- | --- | --- |
| `AFORGE_EXEC_TURNS` | `--max-turns` | iterations |
| `AFORGE_EXEC_BUDGET` | `--token-budget` | tokens |
| `AFORGE_EXEC_TIMEOUT` | `--timeout` | a duration, or a bare number of seconds |

**A flag that was typed always wins** — including `--max-turns 200`, which is a
decision even though 200 is also the default. A variable that is set but is not
a number stops the run and names itself, rather than being silently dropped: a
campaign that thinks it capped every call because of an unnoticed typo measures
the wrong thing all night, and an old spelling counts as typed. A variable set to
an out-of-range value meets exactly the guard the flag has (turns and budget must
be positive; a timeout of zero or less is refused at the flag, naming the
variable that held it).

### Stream discipline

**stdin is the prompt** when no prompt argument was given. **stdout is the
result and nothing else** — one JSON object under `--json`, the deliverable text
otherwise, one trailing newline either way. Every diagnostic goes to **stderr**,
including the provider error behind a failed run. A harness may parse stdout
whole; it never has to strip anything out of it.

### The `--json` envelope — the same object `do` and `run` print

```json
{
  "ok": true,
  "stop": "done",
  "answer": "the answer, in full",
  "files": ["/abs/path/to/any/file/it/wrote"],
  "error": "",
  "spend_usd": 0.0731,
  "tokens": {"in": 48213, "out": 3110},
  "seconds": 184.2,
  "model": "deepseek/deepseek-v4-flash",
  "steps": 9,
  "run": "0123456789abcdef",
  "calls": 9,
  "rounds": 0
}
```

It is the object in section 1, field for field — `exec` and `do` used to print
two different shapes with no vocabulary in common, so a harness wrapping both
wrote two readers and the second one was written wrong.

| Field | Contract, where `exec` differs from section 1 |
| --- | --- |
| `answer` | The deliverable, whole. Empty is possible, and it is now `stop: "incomplete"` and exit `2` rather than the old `stop: "done"` and exit `6`. |
| `stop` | `done`, `error`, `incomplete`, `budget`, `turn-cap`, `deadline`. The executor's own endings that are not rungs of their own — `empty`, `split`, `overrun`, `no-progress`, `promote`, `paused`, `cancelled` — still pass through under their own names and land on exit `2`. |
| `steps` | Iterations of the tool loop. This is what `turns` was. |
| `run` | This invocation's id, as above. |
| `calls` | Model calls this run made. |
| `rounds` | `exec` does not plan and cannot grow: always `0`. |
| `seconds` | Wall clock. This is what `elapsed_ms` was, in seconds. |
| `files` | The files the run wrote as work product, in stable order. The harness's own records — traces, job logs — are deliberately not listed. Always a list, never `null`. This is what `artifacts` was. |
| `tokens`, `spend_usd` | What `usage` carried, split into the two facts a campaign actually reports. |
| `error` | Empty unless `stop` is `error`. A run cut off by `--token-budget`, `--max-turns` or `--timeout` that produced text is a run that produced an answer, so it publishes the answer and no `error`. |
| `incomplete` | Present only when a limit cut the run short: the reason, in the same words stderr carried. The same field `aforge run` carries, with the same meaning. |

**Every old name is still printed beside the new one for one release** — `text`,
`turns`, `elapsed_ms`, `artifacts`, and the whole `usage` object — so nothing
that reads them breaks today. The full before-and-after is
`docs/design/polish/envelope-and-exits.md`.

**One value moved and was not preserved.** `exec` used to report
`"stop": "done"` for a run that finished having produced no text at all. It says
`"stop": "incomplete"` now, because the old value said the work was done about a
run with nothing to show.

**And `error` stopped carrying limits.** A run stopped by its token budget, its
turn cap or its wall used to publish the partial text in `answer` AND the
limit's sentence in `error`, which contradicts what `error` is documented to
mean and left a script written against the contract either discarding a usable
answer or reporting a startup failure that never happened. The sentence is in
`incomplete` now; `error` is filled on `stop: "error"` and on nothing else.

`--out file` writes this same object whether or not `--json` was passed, so a
caller can keep stdout for the prose and still get the machine record.

### Exit codes

`exec` leaves on the **same ladder as `do` and `run`** — the table in section 1.

| Code | `stop` | Means |
| --- | --- | --- |
| `0` | `done` | The worker stopped asking for tools and had something to say. |
| `1` | `error` | It could not be run at all: the provider failed, the key was missing, the model id was rejected. |
| `2` | `incomplete` | It ran and did not finish — including finishing with nothing to show, and every ending without a rung of its own (`cancelled`, `paused`, `promote`, `split`, `empty`, `overrun`, `no-progress`). |
| `3` | `budget`, `turn-cap`, `deadline` | A limit you set stopped it: the token budget, the turn cap, or the wall. `answer` holds whatever it had. |

**This moved a long way, and there is a hatch.** `exec` used to return 2 for the
budget, 3 for the turn cap, 4 for the wall, 5 for an error *and every
unclassified ending*, and 6 for a run with nothing to show — and it never
returned `1`, which is what every other command in the binary returns for "could
not be run at all". That is the whole reason its numbers moved.

```bash
AFORGE_EXIT_CODES=legacy aforge exec "…"
```

restores exactly the old 2/3/4/5/6 **for one release** and changes nothing else:
not `do`, not `run`, not one field of the envelope, not one word on stderr. It
is not a general compatibility mode.

The rule for a harness is the same as for `do`: **read the exit code, or `stop`,
never `answer` alone.** `answer` on a non-zero exit is partial work, not an
answer. `stop` is the field to move a script to: it names why a run ended in a
word, it is the same word on all three commands, and it is not going to move
again.

### What `exec` deliberately does not do

- **No `--db`, no journal, no notebook, no blackboard.** Nothing a run learns
  survives it, and two runs share nothing. If you want state across calls, that
  is `do --db`.
- **No daily spending limit and no `--yes-spend`.** `AFORGE_DAILY_BUDGET` is not
  consulted here; `--token-budget` is the only ceiling, and it is counted in
  tokens. A campaign driving `exec` is responsible for its own spend.
- **No resident lease.** It never waits for another aforge and never hands work
  to one.
- **No delivery gate and no replan.** Nothing judges the answer, and nothing
  notices the work was bigger than one worker.
- **Scratch lands in `--dir`.** `exec` gives the worker no separate scratch
  directory, so the harness's own machinery — `.aforge/`, `.obs/` — is written
  into the workspace beside the work product. Point `--dir` at a directory you
  are willing to have written into, not at a repository you want left clean.

`exec` still reads the state root for two things: the model catalog cache and,
if you have one there, a persisted API key. `AFORGE_HOME` moves both.

---

## 3. Recipes

**One errand, machine-readable, isolated:**

```bash
aforge do "fix the failing test in ./pkg/parse" \
  --dir "$PWD" --json --yes-spend --timeout 15m
```

**A sequence of tasks that must remember each other** — the store is what
carries across them; the working directory carries whatever the tasks did to
it:

```bash
for task in "$@"; do
  aforge do "$task" --db "$RUN/store/graph.db" --dir "$RUN/repo" \
    --json --yes-spend --timeout 15m >> "$RUN/results.jsonl"
done
```

**A disposable brain that touches nothing of yours:**

```bash
AFORGE_HOME="$(mktemp -d)" AFORGE_DAILY_BUDGET=5 \
  aforge do "$TASK" --dir "$REPO" --json --yes-spend
```

**Reading the ending correctly** — branch on `stop`, which is one word and the
same word on all three commands:

```bash
aforge do "$TASK" --json --yes-spend > out.json
case "$(jq -r .stop out.json)" in
  done)               jq -r .answer out.json ;;
  incomplete)         echo "part of it does not stand:"; jq -r .answer out.json ;;
  deadline|price)     echo "a limit stopped it:";        jq -r .answer out.json ;;
  question)           jq -r .blocked_on out.json ;;      # never .answer here
  *)                  jq -r .error out.json ;;
esac
```

---

## 4. The other headless commands

| Command | What it is for |
| --- | --- |
| `aforge chat --once "<text>" [--model slug] [--yolo] [--one-model] [--reasoning level] [--no-compact]` | One conversational turn, non-interactively: the chat surface's brain with the surface removed. See below — it is a different shape from `do`. |
| `aforge plan new "<goal>" [--out plan.json] [--json] [--instructions] [--passes auto\|off\|N]` | Compile a goal to a plan file. For reading and editing a plan by hand. Exits `2` when the plan it wrote still carries a node the ruler measured past one worker and the passes then left whole — see below. |
| `aforge plan run <plan.json> [--dir dir] [--parallel 8] [--out done.json] [--yes-spend]` | Execute exactly what the file says. Byte-stable, no mid-flight thinking. |
| `aforge plan revise <plan.json> "<what happened>" [--done 1,2,3]` | Re-plan from what actually happened. |
| `aforge plan show <plan.json>` | Print a plan. |
| `aforge run <program> --input <file.json\|->` | Run one saved program on typed input. Section 1's exit ladder and envelope. This was `aforge run subharness <name>`. The program is a **bare word**; an argument spelled as a path — a separator in it, a leading `./`, `../` or `~`, or a file extension — is read as a plan file and takes the retired `aforge run <plan.json>` road. It is the shape of the argument and never what is in the working directory. |
| `aforge exec ["<prompt>"] [--dir dir] [--max-turns N] [--token-budget N] [--timeout D] [--json] [--out file]` | One linear worker with no plan behind it — section 2 above. The bottom of the product, for a caller that has already decided what the work is. |
| `aforge version` | The build this binary was cut from. `--version` and `-v` say the same thing. Answers with no API key set, because probing for the binary must not be a configuration problem. |
| `aforge wake [--timeout 2m]` | One full background pass — evaluate sentinels, fire what is due, journal it, exit. What the five-minute timer runs. |
| `aforge doctor` | Labelled rows: `store` and its size, who is resident, the `background timer` and when it last woke, today's spend against the limit, active goals and pending questions. |
| `aforge competence` / `aforge why self` | The measured competence map; today's self-spend receipts. |
| `aforge why <task-id>` | One piece of work's turn-by-turn record: what it said, which tools it called with what arguments, what came back, and how it ended. |
| `aforge notebook [retract\|restore <seq>]` | Inspect, search, and retract beliefs. |
| `aforge services [stop <name>]` | Long-running processes it was asked to keep. |
| `aforge models` | The router ledger — ratings and how many observations back each. |
| `aforge rebuild [--yes]` | Discard everything worked out from the journal and replay it. |
| `aforge help env` | The environment table: every variable and its default. It moved off `aforge --help`, which was 127 lines with more than half of them this table. |

### `aforge plan` — exit `2` means the plan is not settled

`aforge plan new` writes and prints its plan whatever it thinks of it: a plan
with one step too big for the worker that will run it is still the best account
of the goal anyone has, and `--out` and `--json` produce exactly the same bytes
they always did.

What the exit code says is whether the planning finished. **A node sized
`oversized` that carries an `undivided` reason is a piece of planning that did
not finish** — nobody could name two pieces for it, the division gave back the
node again, the depth ceiling arrived first — and the door exits `2` over it,
with one line per node on **stderr**:

```
not settled: North, South, East — no two pieces could be named for it
```

Exit `0` therefore means every step is one the ruler will stand behind. A
harness that reads `$?` and stops there is reading the right thing; one that
reads only "a plan was written" was, until this, told a plan was settled on
103 of 273 measured draws where it was not.

An oversized step with no reason on it is not this: the split gate collapsing a
plan writes its own sentence and takes the responsibility, and the door leaves
that alone.

### `aforge chat --once` — one turn, and what it is not

`--once` is the headless door to the chat surface: it opens the same session
`aforge chat` opens, submits one message, prints the reply, and exits. It is
the right command for measuring *the chat experience*, and the wrong one for
measuring a job.

It differs from `do` in three ways a harness will trip over:

- **It is one turn, not one errand.** No plan is compiled, so there is no
  delivery gate, no replan, no `done.json` and no node count. `do`'s "the
  compiler decides the shape" is exactly the thing that is absent here.
- **There is no `-w`.** The workspace is the process's current directory, so a
  harness cell has to `cd` into the clone rather than point at it.
- **It prints no `$` summary line.** `do` ends with
  `<elapsed> · <n> nodes · $<spend>`; `--once` ends with the reply. The spend
  is in the session transcript instead — `usage` records in
  `$AFORGE_HOME/v3/projects/<slug>/<session>/transcript.jsonl`, one per model,
  each with `costUsd` and a `calls` count, and `aux: true` on the calls made
  beside the turn rather than by it. **Sum `costUsd` across every record**; a
  harness that reads only the un-`aux` one under-reports.

Nobody is watching a `--once` run, so it takes an explicit posture rather than
a default: consent is refused rather than assumed (`--yolo` is how you say in
advance that tool calls may run), and standing items are absent — a clock armed
by an unwatched run would be the harness agreeing on somebody's behalf.

### `--one-model` — the measurement posture

A chat session does not run every call on `--model`. Auxiliary calls resolve
through the tier rows and role pins in `/settings` (`models.tiers.*`,
`models.roles`), so a profile that points `planner` at one model and `reflex`
at another will spend part of every turn there — measured on one trivial task:
22% of its dollars, on a model the run never named.

`--one-model` settles every **text** call on the session model for that run:

```
aforge chat --once "<text>" --yolo --one-model --model deepseek/deepseek-v4-flash-0731
```

It **changes no setting and writes nothing**. The rows are still there and the
next session without the flag reads them exactly as before. What it does is
withhold three inputs, each of which this build has always handled as "unset":

| Input | Withheld | What answers instead |
| --- | --- | --- |
| role pins and tier rows | `RolesSource` is not built | `roles.ResolveCall`'s last rung: the session model |
| `task.model` | passed empty | `defaultTaskModel`: the model the conversation is on right now |
| `models.fallbacks` | passed empty | nothing — no hop to a second model on a failure |

Because these are the ladder's own fall-through states rather than a fourth
resolution path, the flag cannot drift from the behaviour it is settling.

Two deliberate limits:

- **Media slots are untouched.** Vision, image, speech and video are
  capability-qualified — a text model cannot answer `view_image` — so settling
  them on the session model would not make a run single-model, it would make it
  broken.
- **It is refused with `--host`.** Over ssh the far machine owns those rows, and
  a flag that looked like it applied and did not would be worse than no flag.

Standing items never take this posture, whatever the session that created them
was started with: they fire on their own clock long after the measured run
ended.

---

## 5. Environment

The full list is `aforge help env`. What matters headless:

| Variable | Default | Why a harness cares |
| --- | --- | --- |
| `OPENROUTER_API_KEY` | — | Required. |
| `AFORGE_MODEL` | see `--help` | The work model. `--model` overrides per run; unset, the profile's crew answers before the built-in default — see the ladder in section 1. |
| `AFORGE_PLAN_MODEL` | unset | Plans, replans, contracts, and the gate on a stronger model while a smaller one executes leaves. Unset, the profile's crew mastermind answers; with no crew written, the work model plans too. |
| `AFORGE_MODELS` | unset | A panel instead of one model: calls cascade cheapest-first and escalate when a verifier catches a failure. Comma-separated slugs or a JSON path. **Changes what a run costs and how it fails — pin it when measuring.** |
| `AFORGE_DAILY_BUDGET` | `20.0` | The day's spending limit in dollars; `0` is unlimited. A run that reaches it stops. |
| `AFORGE_PREAUTHORIZE_SPEND` | unset | `1` is `--yes-spend` for every run. |
| `AFORGE_HOME` | `~/.aforge` | The whole state root — journal, workspace, CAS, craft, profiles, catalog, skills. One word moves everything; this is the isolation seam. |
| `AFORGE_PROFILE_DIR` | `AFORGE_HOME` | Where measured behaviour is kept. |
| `AFORGE_CONTEXT_FILL_PCT` | `60` | How full any agent's context window may get before it compacts, in percent; clamped 10–90. One law for head turns, planner passes, leaf workers and judges alike. `--context-fill` sets it per run. **Setting it also moves the chat conversation's own fold line**, which otherwise follows the model's window (`/status` says which rule governs); leaving it unset is not the same as setting it to 60. |
| `AFORGE_COMPLETION_RESERVE` | `65536` | Tokens every call keeps free for its answer plus its reasoning. `--completion-reserve` sets it per run. **Pin both when measuring** — they change how much material a call sees and therefore what it costs. |
| `AFORGE_MAX_DEPTH` | `2` | Levels of decomposition. |
| `AFORGE_NODE_BUDGET` | `60` | Hard ceiling on total nodes. |
| `AFORGE_REASONING` | `off` | Planning-call reasoning effort. |
| `AFORGE_EXEC_REASONING` | model default | Executor-call reasoning effort. |
| `AFORGE_EXEC_TURNS` | unset | `aforge exec` only: the turn cap when `--max-turns` was not passed. |
| `AFORGE_EXEC_BUDGET` | unset | `aforge exec` only: the token budget when `--token-budget` was not passed. |
| `AFORGE_EXEC_TIMEOUT` | unset | `aforge exec` only: the wall when `--timeout` was not passed, as a duration or a bare number of seconds. A typed flag always wins over all three; see section 2. |
| `AFORGE_EXIT_CODES` | unset | `legacy` restores `aforge exec`'s old 2/3/4/5/6 exit codes for one release and changes nothing else. |

A variable set in the environment always wins over the `/settings` sheet, and
that row reads read-only in the sheet rather than fighting your shell.

---

## 6. Measuring aforge with this surface

Rules that came from getting them wrong:

- **Read `stop` or the exit code, never `answer` alone.** An empty answer with
  `blocked_on` set is a task that was never attempted; scoring it as a wrong
  answer overstates capability failure and hides an unanswered question.
- **The task string is the prompt under test.** `do` runs it verbatim, so a
  benchmark's phrasing is the phrasing that was measured — no compiler is
  quietly repairing a bad one, and no campaign is comparing two runs on two
  differently-reworded asks. What you leave implicit gets assumed and declared,
  not asked back about; if that matters to your score, say it in the ask.
- **Report `seconds` and `spend_usd` from the JSON**, not from your own wall clock
  around the process — they are measured inside the run, and `spend` is a real
  ledger delta.
- **Do not change host, model, or `AFORGE_MODELS` mid-campaign.** Wall clock and
  cost are reported columns; changing what produces them mid-run corrupts the
  comparison rather than improving it.
- **`--model` alone does not pin a chat cell to one model.** The tier rows and
  role pins answer the auxiliary calls, so a campaign attributing spend and
  quality to a named model must pass `--one-model` — or measure a profile it
  did not record. `do` takes its two seats from the same tier rows when nothing
  else names them, so a headless cell is pinned by passing both flags (or both
  variables), and `model_source` in the `--json` object says whether they took.
  A run whose numbers are compared across the two shapes should say which is
  which. Verify rather than assume: the `usage` records in the session
  transcript name the model that actually served each call.
- **The profile is part of the measurement.** Two cells run from two profiles
  with different crews are two configurations, not one. Record `model_source`
  and `plan_model_source` beside the score, or point every cell at one
  `AFORGE_PROFILE_DIR`.
- **`--timeout` is part of the result.** A cell that hit the wall measured the
  wall as much as the work. Report the timeout rate beside the score or the
  score is not what it appears to be.
- **One `--db` per experimental unit.** Sharing a store across units that were
  meant to be independent leaks learning between them; giving each unit a fresh
  store when the experiment is *about* memory erases the effect being measured.
