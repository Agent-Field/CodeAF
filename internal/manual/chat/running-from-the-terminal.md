# Commands you type in a terminal

## Running aforge from the terminal — can I run this without the chat

Typing `aforge` with no arguments opens the conversation. Everything else is a verb after
it, and there are five kinds:

```
talk to it              chat · resume
hand it work            do "<task>" · exec "<prompt>" · run <program>
look at what happened   why self · why <task-id> · notebook · competence · services ·
                        logs · models · doctor · manual · version
housekeeping            cache · cache clean · rebuild · wake · serve · devices · help env
plan work by hand       plan new "<goal>" · plan show <plan.json> ·
                        plan revise <plan.json> "…" · plan run <plan.json>
```

Two more exist and are deliberately kept out of the help text, because nothing types them
by hand: **`aforge engine`** is the far half of `chat --host`, started by ssh, and
**`aforge tick`** is the one bounded pass the background timer runs every five minutes.
Neither draws anything or reads a key.

## What aforge --help prints — the five groups, and where the environment table went

`aforge help`, `--help` and `-h` all print the same thing: every command under those five
headings, in that order, then five worked examples.

**The environment table is not on that page**: it is `aforge help env`, because it is a
reference somebody consults and it used to be more than half of what `--help` printed.

Every verb also answers `<verb> --help` with its own line and its flags, and `aforge plan
--help` answers with all four of its subcommands.

## What $? means after a headless one-shot — the codes it leaves with

**One table, and `aforge do`, `aforge exec` and `aforge run` all leave on it.**
This is what `$?` holds after a one-shot, and it is the thing a script should branch on:

| `$?` | what it means |
| --- | --- |
| 0 | it is done, and what is on stdout is the answer |
| 1 | it could not be run at all — no key, bad arguments, the store would not open, the name is not a program |
| 2 | it ran and did not finish: part of the work does not stand |
| 3 | a limit you set stopped it — the wall, the token budget, the turn cap, the price |
| 4 | it needs an answer from you and nobody was there |

The three commands used to have three tables, and two of them meant opposite things by the
same number: `do` exit 1 was "nothing usable came back" and `aforge run` exit 1 was "it
could not be run at all", while `exec` returned 2, 3, 4, 5 and 6 and never returned 1. If
you have a script written against the old numbers, `AFORGE_EXIT_CODES=legacy` puts `exec`'s
back for one release — see below — and the rest is in
`docs/design/polish/envelope-and-exits.md`, which sets old and new side by side.

**Exit 3 and exit 2 are different questions.** 3 says the work was going when something you
set cut it off, so raising `--timeout`, `--token-budget` or `--max-turns` and running it
again is the remedy. 2 says it got to the end and part of it does not stand, so what came
back is worth reading before anything is re-run.

**A delivery nothing checked is exit 2 too, and it says so.** `aforge do` puts what a run
delivered to a final check; when that check cannot be reached — a dead route, a refused
account, a service that is down — the work still ships (holding a finished deliverable
hostage to the weather helps nobody), and the run ends `unchecked` rather than `ok`. The
gate is asked once more first, inside the wall, unless there is no time left for a call or
the refusal was about the request itself and every endpoint would say the same. The last
line on stderr is

```
delivered without a check: the gate could not be reached
```

with how it was asked and the provider's own sentence after a `·`. `$?` is 2, `stop` is
`unchecked`, `ok` is false, and `--json` carries the whole reason in `unjudged`. **Read the
answer — it may be perfectly good — but nothing has vouched for it.** Two graded runs
ended `ok` at exit 0 over exactly this before the ending had a name.

**Exit 1 means nothing ran, and only that.** It is the rung for a refusal *before* the work
starts — no key, a flag it could not parse, a store that would not open, a saved program by
that name that does not exist. Nothing was attempted, so nothing was spent and there is
nothing on stdout worth keeping. **A run that started and then failed leaves with 2, not
1**, however early it broke: a provider that gave up at turn nine has already cost you
money and is usually holding part of an answer, and the reason it stopped is in
`incomplete` rather than in `error`. So a script may retry exit 1 blind, and must not do
that with exit 2 — read what came back first. `aforge exec` published mid-run provider
failures as exit 1 for a while; if a wrapper of yours treats 1 as "provider flaked, try
again", that is the line to revisit.

**Exit 4 is the one nobody can fix by retrying.** A headless run has no keyboard, so a
question ends it. The question is printed on stderr verbatim, under `it stopped to ask:`,
and it is in the `blocked_on` field of `--json`. Answer it inside the ask itself and run it
again, or bring the work to `aforge` where it can be answered.

Asking for help is never a failure: `--help` on any verb exits 0.

## The --json result object — one shape, three commands

`--json` on `aforge do`, `aforge exec` and `aforge run` prints **one object on
stdout, always parseable, printed even when the run failed**:

```json
{
  "ok": true,
  "stop": "done",
  "answer": "…",
  "files": ["notes.md"],
  "error": "",
  "spend_usd": 0.0213,
  "tokens": {"in": 18422, "out": 1130},
  "seconds": 91.4,
  "model": "anthropic/claude-opus-4",
  "steps": 3,
  "run": "0123456789abcdef",
  "calls": 47,
  "rounds": 2
}
```

| field | what it holds |
| --- | --- |
| `ok` | the work stands. True on exactly the runs that exit 0 |
| `stop` | why it ended: `done`, `error`, `incomplete`, `unchecked`, `budget`, `turn-cap`, `deadline`, `price`, `question` |
| `answer` | what was produced, in prose. Empty when nothing was |
| `files` | the paths it wrote. Never null — a run that wrote nothing carries `[]` |
| `error` | why it could not be run at all, in the same words stderr carried. Empty on every run that started, however it ended — a limit that cut a run short and a provider that failed mid-run both say why under `incomplete` |
| `spend_usd` | what it cost, whole, in dollars |
| `tokens` | `{"in": …, "out": …}` |
| `seconds` | wall clock |
| `model` | the model the work ran on |
| `steps` | how many pieces of work ran — `do`'s nodes, `exec`'s turns. A saved program does not measure it: the key is still there, holding `0`, and that `0` is a measurement nobody took rather than a count of none |
| `run` | this invocation's id. It names the folder `--debug` writes into, and every row this run wrote into `~/.aforge/logs/calls.jsonl` carries it too — so `aforge logs --run <that id>` is how you get from this object to the calls behind it. Empty on a verb that opened no run of its own |
| `calls` | how many model calls the run made, counted whether or not the call log is switched on. It is the figure you would otherwise count by hand in `calls.jsonl` |
| `rounds` | how many times the run went back for **more work** after looking at what it had. One is the ordinary shape; eight is a run that kept finding more to do, and it is the number that explains a bill nothing else here accounts for. `exec` does not plan and a saved program does not grow, so both hold `0` — a measurement nobody took, the way `steps` does |

**Within a release a field is never removed and never changes meaning; new fields may
appear.** `stop` is the field to read for *why*; the exit code only says how much is wrong.

**None of the three takes `--yolo`.** That is the conversation's flag, and it means "stop
asking me before each tool call" — these three have nobody watching in the first place, so
nothing in them stops to ask. Typing it is refused by name:

```
error: aforge do has no --yolo flag — nothing here stops to ask, and --yes-spend answers the one question a run can still stop on
```

`--yes-spend` is the nearest thing to an equivalent on `do` and `run`: the one thing they
still refuse is a plan whose price crosses your limit. `exec` has no such flag — what bounds
one pass there is `--token-budget` and `--timeout`.

`--json` on `aforge plan new` and `aforge plan revise` is a different thing: it is the plan
itself, the same bytes `--out` would write. `aforge logs --json` is a third: one JSON object per line,
byte-for-byte what is on disk.

## What checked my unattended or headless run — what judged the delivery, and why task.audit is not the answer

An `aforge do` errand's delivery is read at the end by the **delivery gate**. It takes a
reading of the project's own checks before the work and another at the end, maps what you
asked for onto the checks that exercise it, and answers whether the delivery holds.

With `--json`, `judged_by` names that reader when the settled root has a gate row that
is not marked unreachable. An unreadable response still names the reader; read `ok` and
`stop` to learn the outcome. `unjudged` instead names an unreachable gate's reason. A run
with no gate row can omit both keys, so absence alone does not prove a check happened.

`task.audit` is a different road's row and does not reach `aforge do`. It governs work the
conversation hands out with `/task`: a separate, fresh, read-only checker is put in a clean
restore of what the task wrote. Turning that row off produces the report line `nothing
checked this work: the task.audit setting is off`. A headless errand never prints that line,
because the session task engine is not the engine running it; its delivery gate is the
check.

One limitation remains: when a job is broken into several pieces, its gate is journaled
against the piece that delivered rather than the whole that settles them, so `judged_by` is
absent there.

## The old --json field names — deliverable, text, elapsed_ms, settled

**The old names still work, for one release, and then go away.** They are printed beside
the new ones, so nothing that reads them breaks today and nothing has to be rewritten in a
hurry:

| old name | read this instead |
| --- | --- |
| `deliverable` (`do`), `text` (`exec`) | `answer` |
| `artifacts` | `files` |
| `spend` (`do`) | `spend_usd` |
| `nodes` (`do`), `turns` (`exec`) | `steps` |
| `elapsed_ms` (`exec`) | `seconds` |
| `usage` (`exec`) | `tokens` and `spend_usd` |

**`settled` is not the old name of `ok`, and it is not going away.** It means "nothing this
run is waiting for can still move", which is true of a run that asked a question and did
nothing: `settled: true` with `ok: false` and exit 4. Reading the one as the other would
record every refusal as a success. A broken finished tree is the one ending that answers
that sentence false; *Why settled can be false* below gives its exact shape.

## Why settled can be false — the tree does not build or the run left code broken

A run that hands back a tree its own check could not collect is not settled: the tree does
not build, it left the code broken, and repairing it is still work waiting to move. That
run says `settled: false`, `ok: false`, `stop: "incomplete"`, and leaves with exit 2. Its
answer includes the check's own sentence about what could not be read.

## Fields that belong only to one command

Some fields belong to one command and stay. `aforge do` carries `spend_work` and
`spend_overhead` — what the work cost against what it cost to decide what the work should
be — and `blocked_on`, `learned`, `plan_model`, `model_source`, `plan_model_source` and
`subharness`. It also carries `judged_by` when the settled root records an answered gate
attempt and `unjudged` when that gate could not be reached. Both keys can be absent when
no root gate row is available; neither key replaces `ok` and `stop`. `aforge run` carries `output`, which is
the typed answer whole, and `report`.

`incomplete` is on `aforge run` and `aforge exec` both, and it is why it did not finish, in
the same words stderr carried — a token budget that ran out with half an answer already
written, a wall that arrived, a provider that gave up at turn nine. It is **not** `error`:
`error` means the run could not be started at all, and every one of those started. A run
that was cut short leaves `error` empty, puts what it managed in `answer`, names the reason
in `stop`, and says the sentence in `incomplete`.

## Keeping exec's old numbers for one release — the legacy switch

`aforge exec` used to leave with 2 for the token budget, 3 for the turn cap, 4 for the wall,
5 for an error and 6 for a run that finished with nothing to show, and it never returned 1.
Those five numbers all moved when the three commands were put on one table.

Setting `AFORGE_EXIT_CODES=legacy` puts them back:

```
AFORGE_EXIT_CODES=legacy aforge exec "…"
```

**It changes nothing else.** Not `aforge do`, not `aforge run`, not one field of `--json`,
not one word on stderr. It is an escape hatch for scripts already written, it applies to
`aforge exec` and to nothing else, and it goes away after one release. The thing to change
the script to is `stop` in `--json`, which names why a run ended in a word rather than a
number and is the same word on all three commands.

## What does aforge wake do — running the background pass by hand, once

`aforge wake` does one bounded pass of the work that normally happens on the five-minute
timer, prints what it did, and exits. **It starts no permanent process and no worker
runner.** Unlike everything else on this page that only reads, it spends: it makes model
calls.

```
aforge wake [--db path] [--timeout 2m]
```

`--timeout` defaults to **2m**. It takes a duration — `--timeout 5m`, `--timeout 90s` — and
a bare number is still read as seconds, so `--timeout 120` is the same wall. Zero or less
is refused at the flag: `--timeout: must be positive`. Inside that wall it takes at most
**32** passes, and stops early the moment a pass changes nothing.

The flag used to be `--max-seconds`, which was the same wall spelled a third way and in the
unit rather than in the quantity: `aforge wake --max-seconds 5m` was a parse error on a
machine where `aforge do --timeout 5m` works. **`--max-seconds` still works for one
release** and says so on stderr the first time it is used:

```
note: `--max-seconds` is now `--timeout` — the old spelling works for one more release.
```

**It defers to a live one.** If something else already holds the role for that store, it
prints one line and does nothing:

```
resident alive (pid 41207) — skipping wake
```

The exception is a holder that is alive but has stopped completing passes. Waiting on that
forever is how a stalled process quietly stops every check on the machine, so it says so
and goes anyway:

```
resident (pid 41207) has not ticked since 2026-09-02T11:04:18Z — waking anyway
```

The last line counts what the pass did:

```
examined 3, checked 2, fired 1, no 0, errors 0, rail waits 0, practice 0, learning 2
```

## What did that task actually do — see one piece of work's turn-by-turn record, with aforge why

This prints one piece of work's whole record: every turn, what it said, every tool it
called with its arguments, what came back, and how it ended.

```
aforge why <node-id> [--db path]
```

Each entry is a headline with its body indented four spaces under it:

```
turn 1 · said
turn 2 · read ←
turn 2 · read → 12ms
turn 3 · bash → 1.4s · error
turn 4 · aforge
turn 5 · stopped
turn 6 · the record stops here
```

`←` is the call going out; `→` is what came back, with how long it took — milliseconds
under a second, tenths of a second above. `· error` marks a tool that failed. A turn signed with the product's own name is a note
about the run rather than something the model said, and `the record stops here` means the
record was trimmed.

**When there is nothing to print it says which nothing it is**, because those are two very
different situations:

```
build has no transcript: either nothing has run it yet, or the worker that ran it keeps no record.
```

**And it leaves with 1**, not 0. The sentence is for you; the exit code is for the script
that asked, which would otherwise read a success and conclude the id exists and has nothing
in it. `aforge logs --run <id>` answers a miss the same way.

The id is the one the plan gave that step — the same id `logs --node <id>` filters on, and
the value of the `node` field in `logs --json`.

## Where is the record of my headless run — reading a kept one-shot's store

`why` reads a store, and by default that store is `~/.aforge/graph.db`. A headless
`aforge do` run does **not** work there: it uses a private store of its own, kept only when
the run failed or you asked for it with `--keep`, and the last line on the error stream
says where:

```
record kept at ~/.aforge/runs/aforge-do-3f81c2
```

That directory holds a `graph.db`, and that is what to point the reader at:

```
aforge why <node-id> --db ~/.aforge/runs/aforge-do-3f81c2/graph.db
aforge why self      --db ~/.aforge/runs/aforge-do-3f81c2/graph.db
```

`--db` means the same thing on `why`, `notebook`, `competence`, `services`, `doctor`,
`rebuild`, `wake` and `do`. **None of them will create a store**: a `--db` that is not a
regular file is refused, each in its own words — `open receipts: <path> is not a regular
database file` from `why`, `open notebook: …`, `open competence map: …`, `open store: …`
from `rebuild`, `open wake store: …`.

**A conversation's tasks are somewhere else entirely.** A task commissioned in the chat
writes its transcript to a file beside the conversation, not into this store, and its room
in the chat replays it. `why` is the reader for headless work and for the background pass.

## What have I spent today — the receipts, with aforge why self

`aforge why self` prints today's receipts: what was attempted, what it cost, and what was
learned from it. The day is local midnight to now.

```
aforge why self [--db path]

TRIED                          COST     LEARNED
Practice parser recovery       $0.31    facts #14,#15; surprise down 12%
Summarise the changelog        $0.0042  nothing
```

- **TRIED** is the intent the work was started with, folded onto one line.
- **COST** is dollars, to cents, and to four decimals when it is under a cent.
- **LEARNED** lists `facts #…` and `skills #…` by their notebook numbers, then
  `surprise down N%` when the work became more predictable. When nothing came of it the
  column says `nothing`, and work that became *less* predictable adds `surprise up N%`
  after it.

**A day with nothing on it says `nothing was tried on its own account today.`** It used to
print the `TRIED  COST  LEARNED` header with no rows under it, which reads as a table whose
rows failed to arrive rather than as a quiet day.

## What has it learned — reading and retracting beliefs with aforge notebook

`aforge notebook` prints every belief in the store, newest last, with the evidence for it:

```
SEQ  SCOPE      KIND        AGE     USES  RIDES  BAD  STATUS       BELIEF
#14  tool:git   lesson      2d ago  6     4      0    active       always verify changes
#15  user       preference  5h ago  1     0      0    quarantined  prefer compact tables
```

`USES` is how often it was retrieved, `RIDES` how often it rode along into a piece of work,
`BAD` how often that work then failed. `STATUS` is one of `candidate`, `active`,
`superseded`, `quarantined`. Under the table is the day's spending line — one of
`daily rail: unlimited`, `daily rail: $500.00`, `today's spend: $1.23 of $500.00 daily rail`
or `today's spend: $1.23; daily rail unlimited`. A day that has spent nothing prints no
figure for it. Scope renamings follow under `aliases`, as `from → to`.

**To take one back:**

```
aforge notebook retract 15      quarantined #15: prefer compact tables
aforge notebook restore 15      restored #15: prefer compact tables
```

Retracting hides a belief from everything that would otherwise reach for it; restoring puts
it back. Nothing is deleted and the number never changes. A number that is not there is
refused with `notebook fact #15 not found`, and anything that is not a positive number with
`invalid notebook fact sequence "x"`. The `#` is optional. Any other word gets
`unknown notebook command "forget"`.

## What is it actually good at — the measured evidence, with aforge competence

`aforge competence` sorts what this machine has evidence about into four groups, printed in
this order: `strong`, `frontier`, `weak`, `stale`.

```
strong
  repo:/work/parser — 24 runs · 4% failed · surprise flat · 2 installed skills
frontier
  linear work — 6 runs · 33% failed
stale
  repo:/work/old-api — last touched 3w ago
```

Each row names what the evidence is about, then the evidence: how many runs, what share of
them failed, which way surprise is trending, and how many skills are installed there. A
scope with skills that has never been exercised says `installed, not yet exercised` instead
of a failure rate. A `stale` row says only when it was last touched, because an old
measurement is not a measurement.

`--model <slug>` picks whose measured behaviour to fold in; left alone it is the model this
machine would run work on. **On a machine with no evidence the whole answer is one line:**

```
No competence evidence yet.
```

## What is still running in the background — aforge services, and stopping one

`aforge services` lists the long-running processes started here and not yet stopped — a dev
server, a watcher — one per line, tab separated: the name, the status, how long it has been
up, what is watched for health, and where its log is.

```
dev-server	running	2h	port:5173	/tmp/dev.log
```

**With nothing running it says `nothing is being kept running.`** It used to print
absolutely nothing and exit 0, which is indistinguishable from a command that broke — so it
answers in a sentence now, the way `aforge cache` always has.

To stop one, name it:

```
aforge services stop dev-server

dev-server	stopped
```

A name nothing answers to is refused — `service "dev-server" is not running` — and anything
other than `stop <name>` gets the usage line
`usage: aforge services [--db path] | aforge services stop <name> [--db path]`.

## aforge rebuild — throwing away everything worked out from the journal and replaying it

Everything in the store except the journal was worked out from the journal, and can be
thrown away and worked out again. `aforge rebuild` is that, and it is the recovery path
when something in the store looks wrong.

```
aforge rebuild [--db path] [--yes]
```

**It asks first**, on two lines, and only `y` or `yes` proceeds — anything else, an empty
line included, says `cancelled` and changes nothing:

```
Rebuild everything aforge worked out from the journal in /home/you/.aforge/graph.db?
The journal itself is untouched; everything worked out from it is discarded and replayed. [y/N]
```

**The question is on the error stream, and so is `cancelled`.** Only the line saying what
was replayed goes to stdout — so `aforge rebuild | tee log` still shows you the question
and still lets you answer it, and the file gets the result and not the prompt.

`--yes` skips the question for a script. When it is done it says what it replayed:

```
rebuilt 128 steps from 4173 journaled events
```

**It refuses while another process holds that store**, because the rebuild is one
transaction but the process on the other side would be reasoning about a graph that moved
underneath it:

```
a resident is running (pid 41207) — close it before rebuilding
```

Conversations, settings and credentials were not worked out from the journal and are not
touched.

## What goes to stdout and what goes to stderr — piping a headless command

**stdout is the answer. Everything else is on stderr.**

The answer is the thing you would capture: the deliverable, the `--json` object, the rows of
`aforge logs`, the plan `aforge plan show` prints. Everything a person reads *about* the
run is on stderr: the `goal:` and `models:` preamble, the progress lines, warnings, the
path a record was kept at, the receipt saying a file was written, and any question the
command asks you. For `aforge chat --once`, this includes every handover and ending line:
`finishing here · what was asked is done`, `stopping here · ` and the line that says a
reply's work was moved. Landing-woken replies remain on stdout with the first reply.

So these do what you would expect, and nothing has to be filtered out of them:

```
aforge do "summarise CHANGELOG.md" --json | jq -r .answer
aforge plan new "ship the endpoint" --json > plan.json
aforge plan run plan.json > result.txt          # the preamble stays on your terminal
aforge logs --tail 20 | wc -l                   # 20, not 21
aforge cache clean | tee clean.log              # you can still see the question
```

Three of those used to be wrong. `aforge plan new` and `aforge plan run` printed their
`goal:`/`workspace:`/`models:` preamble into the stream; `aforge logs` printed the log's
path as a first line, so every count was one too many; and `aforge cache clean` printed its
**question** to stdout, which put the question in the file and left you looking at a blank
terminal waiting for a word you could not see.

`2>/dev/null` silences the commentary and keeps the answer. To keep both separately, redirect
them separately: `aforge do "…" >answer.txt 2>notes.txt`.

**A command with nothing to show says so in one short sentence, and never prints a column
header with no row under it.** `the cache is empty · <path>`, `nothing is being kept
running.`, `nothing was tried on its own account today.` Silence and a bare header both read
as a command that broke.

## Which of these cost money, and which need no API key

**These read, need no key and spend nothing**: `why`, `notebook`, `competence`, `services`
(listing), `doctor`, `logs`, `cache`, `show`, `manual`, `version` and `--help`. They are
safe in a shell prompt, a CI step or a bug report.

`models` reads the measured ratings on this machine and also reaches the network for the
model catalog, but spends nothing of yours.

**These spend**, because all of them call a model: `chat`, `do`, `exec`, `run`,
`plan new`, `plan revise`, `plan run` and `wake`. Without a key each fails at the door with the same two lines:

```
aforge needs a model to work with.
export OPENROUTER_API_KEY (or OPENAI_API_KEY) and run it again.
```

**`OPENROUTER_API_KEY` is not required** — it is the first of three places a key is looked
for. The variable, then `OPENAI_API_KEY`, then the key kept in your profile, which is where
the one you pasted on the first run or typed into `/settings` lives. Any one of them is
enough, so a machine set up in the chat runs `aforge do` with no variable set at all.
`aforge doctor`'s first row says which one answered — `key set · OPENROUTER_API_KEY`, or
`key set · /home/you/.aforge/config.json`, or `key none ·` and the two lines above.

**These change state without spending**: `cache clean`, `rebuild`, `notebook
retract|restore`, `services stop` and `devices revoke`. The two that destroy something ask
first — `cache clean` wants the word `now` typed out, the same word `/cache clean now`
wants in the chat, and `rebuild` wants `y` — and `--yes` skips the question on both. The other three act at once, and all three can be undone: a
retracted belief restores, a stopped service starts again, a revoked device pairs again.

## Reading a plan by hand — aforge plan new, show, revise and run

`aforge plan` writes a plan to a file you can read, edit and diff, then runs it exactly as
written. It is a real feature and it is not what most people want, because nothing aforge
learns mid-flight can change a plan that is already frozen.

```
aforge plan new "<goal>" [--out plan.json] [--dir dir] [--json] [--instructions]
                         [--passes auto|off|N] [--model slug] [--plan-model slug]
aforge plan show <plan.json>
aforge plan revise <plan.json> "<what happened>" [--done 1,2,3] [--out plan.json]
aforge plan run <plan.json> [--dir dir] [--parallel 8] [--out done.json] [--yes-spend]
                            [--max-turns N] [--token-budget N] [--total-token-budget N]
                            [--no-method]
```

`aforge plan show` is the reader. It takes a plan file and prints `goal:` and then the plan
as a table: the commitments it settled on, the stages, one row per step with what it waits
for, and the instructions under them. It reads a file and nothing else, so it needs no key
and spends nothing. With no file it says `usage: aforge plan show <plan.json>`.

`aforge plan revise` takes the same file and an account of what happened, and writes back a
plan changed in the light of it; `--done` names the steps that already landed.

`aforge plan run` executes the file. `--parallel` is how many steps run at once,
`--max-turns` a backstop per step, `--token-budget` a token wall per step and
`--total-token-budget` one for the whole run, and `--no-method` skips writing a working
method for each step before it runs.

This is a different thing from `aforge run <program>`, which runs a saved program on typed
input. The two used to share the word `run` and share nothing else.

## The old spellings — what happened to plan, show, revise and run

**`run` used to mean two unrelated commands.** `run graph.json` executed a static plan and
`run subharness <name>` ran a saved program. It means the saved program now, matching
`/subharness <name>` in the chat, and the pipeline moved under the one noun its four verbs
all act on:

| what you used to type | what it is called now |
| --- | --- |
| `aforge run subharness <name>` | `aforge run <name>` |
| `aforge run <plan.json>` | `aforge plan run <plan.json>` |
| `aforge plan "<goal>"` | `aforge plan new "<goal>"` |
| `aforge show <plan.json>` | `aforge plan show <plan.json>` |
| `aforge revise <plan.json> "…"` | `aforge plan revise <plan.json> "…"` |

**Every old spelling still works for one release.** It is absent from `--help`, it does
exactly what it always did, and it prints one line on stderr the first time it is used:

```
note: `aforge run subharness <name>` is now `aforge run <name>` — the old spelling works for one more release.
```

**That line is on stderr and never on stdout**, so `run <name> --json | jq` keeps parsing.
The two are told apart by the SHAPE of what you named, never by the directory you stand in:
a first argument spelled as a path is the old pipeline spelling, and a bare word is a
program. A separator anywhere in it, a leading `./`, `../` or `~`, or a file extension on
the end: any of those is a path, and where both readings would work the path wins, because
that is the one you spelled on purpose. `run formatter` is the saved program from every
folder; `run ./formatter`, `run plans/formatter` and `run plan.json` are plan files from
every folder; a saved program whose name carries a dot, `tidy.up`, is read as a file,
because saved-program names are bare words. It used to be decided by whether the file
existed, which made one command mean two things in two folders.

## Which flags moved — budget, turns, brief, contracts, ensemble

Some flag names moved in the same change, for the same reason: one concept, one spelling,
on every command.

| what you used to type | what it is called now | why |
| --- | --- | --- |
| `--budget N` | `--token-budget N` | *budget* is a word about **money** everywhere else here — `AFORGE_DAILY_BUDGET`, `/budget`, `--max-cost` — so `--budget 150000` read as $150,000 |
| `--run-budget N` | `--total-token-budget N` | the same, for the whole-run wall |
| `--turns N` | `--max-turns N` | it is a limit, and every other limit says so |
| `--max-seconds N` | `--timeout 2m` | one duration flag, one spelling, on `do`, `exec`, `plan run` and `wake` |
| `--brief` | `--instructions` | it writes a self-contained instruction for every step |
| `--contracts=false` | `--no-method` | a boolean that defaults on needs a negative spelling, and what it turns off is a **working method** |
| `--ensemble 0\|-1\|N` | `--passes auto\|off\|N` | a tri-state is words, not magic integers |
| `-w`, `-o`, `-j` | `--dir`, `--out`, `--parallel` | the single letters are shorthands and **keep working forever**, silently; the long names are what is printed |

Each renamed flag says the same one line on stderr the first time it is used, and each old
spelling goes away after one release. `--plan-model` on `exec` is the odd one: that command
plans nothing, so the flag is still accepted and now says
`note: exec does not plan — --plan-model has no effect here.` rather than quietly doing
nothing.

**A flag that does not exist, and a number that will not read, are both refused in this
surface's own words** — spelled with the two dashes you typed, never the one dash Go's flag
package writes:

```
error: aforge do has no --nosuchflag flag
error: invalid value "notanumber" for flag --max-turns: a whole number of turns to allow, such as 200
```

`--max-turns`, `--turns`, `--token-budget`, `--budget` and `logs --tail` all answer that way;
a negative count is refused too. The command's own usage follows the line, and the exit is 1
— nothing was attempted.

## Is this install healthy — aforge doctor, and where it keeps things

`aforge doctor` is the page to open when nothing works. It reads this machine and prints a
short block of labelled rows. It needs no key and spends nothing.

```
aforge doctor [--db path]
```

Every row is labelled in the words a developer would search for:

```
store            /home/you/.aforge/graph.db · 496 KiB
resident         this terminal while open
background timer not installed · last wake not yet
spend            rail $20.00
model calls      /home/you/.aforge/logs/calls.jsonl · 26 KiB
```

`store` names the file the journal and every derived table live in, and how big it is —
`· not created` on a machine that has not made one yet. `background timer` is the row about
the five-minute pass: whether it is installed, when it last woke, when it next checks, and
`· checks look stalled` when an installed one has not woken for several cadences. `spend`
is the day against its limit, and `model calls` is where the call log is and what it
weighs. A machine with charters or unanswered questions on it prints a `standing` row too.

Both of the first two labels are recent. `store` used to read `brain` — nobody looking for
where their data lives searches for a brain — and `background timer` used to be named after
a piece of the *other* product in this binary, which is not a thing the chat has at all.

**It leaves out what it has not measured.** A machine that has spent nothing today prints
the limit and no figure beside it, rather than `$0.00` — a zero nobody measured reads as a
machine that counted, which is the opposite of the truth. Same for the counts beside it:
nothing to say is nothing printed.

That command is about this *install*. `/status` in the chat is about the conversation you
are in. They are related and they are not the same reading.

## aforge help, and the verbs that take no flags at all

`aforge help` is a third spelling of `aforge --help`, beside `-h`, and all three print the
same list of commands. Every verb answers for itself the same way — `<verb> --help`, on
stdout, exiting **0**, because asking for help is not a failure — and the fuller account of
that, and of what a mistyped command is answered with, is on the *commands* page.

Two things that account does not cover:

- **The doors that parse no flags at all answer the gesture too.** `aforge plan show`,
  `aforge models` and `aforge cache` take a positional or nothing, and each reads `--help`
  as the question rather than as an argument. `aforge plan show --help` used to answer
  `open --help: no such file or directory` — a filesystem error about a flag.
- **`help env` is the environment table.** It moved off `--help` when that page was 127
  lines and more than half of them were this table, so the last thing on the screen after
  asking what the commands are was `AFORGE_CALL_LOG_BODIES`.
- **`aforge manual --help` prints its usage, then the list of pages.** The list is what
  that command can be asked for, so it is still there; it used to be *all* that was there,
  which made one verb in the binary answer `--help` differently from the other twenty-two.
