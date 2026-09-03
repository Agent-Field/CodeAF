# Commands you type in a terminal

## Can I run this without the chat — every command the terminal answers to

Typing `aforge` with no arguments opens the conversation. Everything else is a verb after
it, and there are five kinds:

```
talk to it              chat · resume
hand it work            do "<task>" · exec "<prompt>" · run subharness <name>
read a plan by hand     plan "<goal>" · show <graph.json> · revise <file> "…" · run <graph.json>
look at what happened   why self · why <node-id> · notebook · competence · services ·
                        logs · models · doctor · manual · version
housekeeping            cache · cache clean · rebuild · wake · serve · devices
```

Two more exist and are deliberately kept out of the help text, because nothing types them
by hand: **`aforge engine`** is the far half of `chat --host`, started by ssh, and
**`aforge tick`** is the one bounded pass the background timer runs every five minutes.
Neither draws anything or reads a key.

`aforge help`, `--help` and `-h` all print the same thing: every command, then the
environment table. Every verb also answers `<verb> --help` with its own line and its flags.

## What does aforge wake do — running the background pass by hand, once

`aforge wake` does one bounded pass of the work that normally happens on the five-minute
timer, prints what it did, and exits. **It starts no permanent process and no worker
runner.** Unlike everything else on this page that only reads, it spends: it makes model
calls.

```
aforge wake [--db path] [--max-seconds N]
```

`--max-seconds` defaults to **120**. It is a wall in whole seconds, not a duration —
`--max-seconds 5m` is a parse error — and zero or less is refused with
`wake max-seconds must be positive`. Inside that wall it takes at most **32** passes, and
stops early the moment a pass changes nothing.

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

`aforge why <node-id>` prints one piece of work's whole record: every turn, what it said,
every tool it called with its arguments, what came back, and how it ended.

```
aforge why <node-id> [--db path]
```

Each entry is a headline with its body indented four spaces under it:

```
turn 1 · said
turn 2 · read ←
turn 2 · read → 12ms
turn 3 · bash → 1.4s · error
turn 4 · the harness
turn 5 · stopped
turn 6 · the record stops here
```

`←` is the call going out; `→` is what came back, with how long it took — milliseconds
under a second, tenths of a second above. `· error` marks a tool that failed. `the harness`
is a note written by the machinery rather than by the model, and `the record stops here`
means the record was trimmed.

**When there is nothing to print it says which nothing it is**, because those are two very
different situations:

```
build has no transcript: either nothing has run it yet, or the worker that ran it keeps no record.
```

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

**A day with no receipts prints the `TRIED  COST  LEARNED` header and no rows.** That is
the command working, not failing.

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

**With nothing running it prints absolutely nothing and exits 0.** A blank answer here is a
healthy machine, not a broken command.

To stop one, name it:

```
aforge services stop dev-server

dev-server	stopped
```

A name nothing answers to is refused — `service "dev-server" is not running` — and anything
other than `stop <name>` gets the usage line
`usage: aforge services [--db path] | aforge services stop <name> [--db path]`.

## aforge rebuild — throwing away every derived table and replaying the journal

Everything in the store except the journal is derived from the journal, and can be thrown
away and rebuilt from it. `aforge rebuild` is that, and it is the recovery path when a
table looks wrong.

```
aforge rebuild [--db path] [--yes]
```

**It asks first**, on two lines, and only `y` or `yes` proceeds — anything else, an empty
line included, prints `cancelled` and changes nothing:

```
Rebuild every materialized view in /home/you/.aforge/graph.db from the event journal?
The journal itself is untouched; everything derived from it is discarded and replayed. [y/N]
```

`--yes` skips the question for a script. When it is done it says what it replayed:

```
rebuilt 128 nodes from 4173 journaled events
```

**It refuses while another process holds that store**, because the rebuild is one
transaction but the process on the other side would be reasoning about a graph that moved
underneath it:

```
a resident is running (pid 41207) — close it before rebuilding
```

Conversations, settings and credentials are not derived tables and are not touched.

## Which of these cost money, and which need no API key

**These read, need no key and spend nothing**: `why`, `notebook`, `competence`, `services`
(listing), `doctor`, `logs`, `cache`, `show`, `manual`, `version` and `--help`. They are
safe in a shell prompt, a CI step or a bug report.

`models` reads the measured ratings on this machine and also reaches the network for the
model catalog, but spends nothing of yours.

**These spend**, because all of them call a model: `chat`, `do`, `exec`, `run`, `plan`,
`revise` and `wake`. Without a key each fails at the door with the same two lines:

```
aforge needs a model to work with.
export OPENROUTER_API_KEY (or OPENAI_API_KEY) and run it again.
```

**These change state without spending**: `cache clean`, `rebuild`, `notebook
retract|restore`, `services stop` and `devices revoke`. The two that destroy something ask
first — `cache clean` wants the word `clean` typed out, `rebuild` wants `y` — and `--yes`
skips the question on both. The other three act at once, and all three can be undone: a
retracted belief restores, a stopped service starts again, a revoked device pairs again.

## Reading a plan by hand — writing a task graph to a file with plan, show and revise

The plan pipeline writes a task graph to a file you can read, edit and diff, then runs it
exactly as written. It is a real feature and it is not what most people want.

```
aforge plan "<goal>" [-o graph.json] [-w dir] [--json] [--model slug] [--plan-model slug]
aforge show <graph.json>
aforge revise <graph.json> "<what happened>" [--done 1,2,3] [-o graph.json]
aforge run <graph.json> [-w dir] [-j 8] [-o done.json]
```

`aforge show` is the reader. It takes a graph file and prints `goal:` and then the plan as
a table: the commitments it settled on, the stages, one row per step with what it waits
for, and the briefs under them. It reads a file and nothing else, so it needs no key and
spends nothing. With no file it says `usage: aforge show <graph.json>`.

`revise` takes the same file and an account of what happened, and writes back a plan
changed in the light of it; `--done` names the steps that already landed.

This is a different thing from `run subharness <name>`, which runs a saved program on typed
input and shares only the word `run`.

## Is this install healthy — aforge doctor, and where it keeps things

`aforge doctor` is the page to open when nothing works. It reads this machine and prints a
short block of labelled rows. It needs no key and spends nothing.

```
aforge doctor [--db path]
```

The first row names the store file and how big it is — `/home/you/.aforge/graph.db · 496 KiB`,
or `· not created` on a machine that has not made one yet. Under it are what is holding
that store, how the background checks are doing, the day's spending against the limit, and
where the model-call log is with what it weighs.

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

- **The doors that parse no flags at all answer the gesture too.** `aforge show`,
  `aforge models` and `aforge cache` take a positional or nothing, and each reads `--help`
  as the question rather than as an argument. `aforge show --help` used to answer
  `open --help: no such file or directory` — a filesystem error about a flag.
- **`aforge manual --help` answers differently on purpose.** It prints the list of pages,
  because the list is what that command can be asked for.
