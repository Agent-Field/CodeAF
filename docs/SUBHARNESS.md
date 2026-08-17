# Sub-harnesses — subharness/ (design)

## Object model
A sub-harness is a durable registry entry (`~/.aforge/harnesses/<name>.hjson`) with: identity (name, desc, author, version int), program (DAG over node kinds), tool whitelist, verify (ladder arg: accept/schema/invariants/loop/report/rederive/adversarial/human), dynamism (fixed/branch/width/metaprompt/recursive/selfmod; integer cap), tests, run history (each run emits a trace dag JSON). Version is a pointer (v1, v2, ...).

## Node kinds (registry)
`agent.loop` (internal/session/loop orientated model+whitelist), `tool.call`, `parallel.split/join`, `branch`, `loop.until`, `human.gate`, `verify`, `subharness.call`, `trigger` (hosted/idle/watch/source.command). Library-in-binary, not generated code.

## As built — internal/subharness
Pages are strict JSON, not hjson: nothing in the tree parses hjson and a page is written by this package and by the distiller, so a format with one reader is not worth a dependency. Layout is `~/.aforge/harnesses/<name>/v1.json`, `v2.json`, … with runs beside them under `<name>/run/<ts>.json`. There is no head file — the head is the highest page present, so a pointer cannot disagree with the pages it points at.

The two ladders are enforced, not annotated. Each node kind declares the lowest dynamism rung a harness may hold it at (`branch`/`loop.until` need branch, `parallel.*` need width, `subharness.call` needs recursive), so a fixed harness cannot quietly contain a decision; `Dyn.Cap` is a whole-run budget spent by loop rounds past the first, and a run that spends it stops deciding rather than failing. A verify node may verify below its harness's rung but never above it, and a rung above `accept` needs something in the program that could keep the promise.

Validate is one function because the checks are not independent (kind → fields, rung → kinds, whitelist → tool names), and Save validates before writing, so every page on disk is one the runner can run. `Run` walks the shape and hands each node to an `Exec` the caller owns — the session loop, the tool call, the ask — which is what keeps this a registry and not a second engine. Its output is the condensed trace: `Trail` per executed step (a loop's rounds are separate steps) plus the edges actually taken.

## Adoption from Agent-Field skill
Autonomy spectrum (typed-vs-loop annotation per node), verification ladder as arg, dynamism ladder + budgets: output-traces of executed loops saved per run. Meta-point: templates derive from the problem, never from a menu.

## How a harness is reached — detection, not a slash command

There is no `/research`. A sub-harness is reached by saying what you want, and
the turn itself is the trigger. The reason is the meta-point above, one rung
further: a registry you have to name is a menu, and a menu is a thing people
forget they have — the harness that exists to do research properly would sit
unused beside a turn doing research badly.

### The registry entry's half

Two fields on the identity carry this, both written at build time by whoever
designs the harness (`internal/subharness`'s `Entry`):

- **`Description`** — one sentence saying what the harness does, in the words a
  person would use for it. It is what the offer shows, and its content words are
  the weaker of the two matching signals.
- **`Cues []string`** — the trigger vocabulary: `["research", "find out", "dig
  into"]`. Single words and phrases; a phrase matches only as a consecutive run
  and is worth more than a lone word.

A harness described as "does stuff" with no cues is never offered. That is the
designer's answer to receive, not a defect for the matcher to work around.

### The matching pass — `internal/subharness/detect.go`

`Score(Turn, Entry) → float64` is **pure and deterministic**: same turn, same
entry, same number, on every machine, forever. No model call, no embedding, no
network, nothing per-turn that costs money. Three reasons, in order:

1. **A model call per turn is a tax on every turn.** Most turns are not a
   harness; paying a small model to say so would put latency and a bill on the
   ordinary case to serve the rare one.
2. **An unrequested question must be predictable.** This interrupts somebody. A
   card that appears for a sentence and not for the same sentence tomorrow is
   worse than no card, because there is nothing to learn about when it happens.
3. **The designer already knows.** The cues were written at build time; asking a
   model to re-derive them at runtime is paying for an answer we have.

The cost is recall — a turn phrased in words no designer wrote is never offered
— and that is the right way round: a missed offer costs a person nothing, and a
wrong offer costs them a question, an answer, and trust in every card after it.

The score is independent evidence combined as `1 − Π(1 − w)`:

| signal | weight | fires alone? |
| --- | --- | --- |
| the harness named out loud (its name **and** the word "harness") | 0.9 | yes |
| one multi-word cue phrase, matched consecutively | 0.7 | no — needs one corroborating word |
| one single-word cue | 0.55 | no |
| two cues | 0.80 | yes |
| description overlap (share of its content words present in the turn) | ×0.5 | never, even quoted whole (0.5) |

`Threshold = 0.7`, **exceeded**, not met. Endings are tolerated to four suffixes
(`researching` → `research`, `digging` → `dig`); nothing else is stemmed. A name
that is also a cue counts once, at the stronger reading. `Best` returns the
highest-scoring entry, ties going to registry order, so the same sentence asks
the same question twice.

### The routing — `internal/session/loop.go` → `harness.go`

Once per turn, at the top of `runTurn`, before the first provider request:

```
turn recorded → routeHarness → Best(turn, registry) > 0.7 ?
   no  → the ordinary turn, untouched
   yes → EventHarnessOffer  ──▶  one-row card, held on the answer
             no  → the ordinary turn, untouched
             yes → EventHarnessRun → Config.RunHarness → report recorded
                   as the turn's assistant message → EventTurnDone
```

Four laws hold it:

- **It is a question, never a routing.** Nothing runs because a matcher said so.
  Detection cannot lose a turn: the no is free and the ordinary turn is already
  recorded and about to be sent.
- **It is asked once per turn, here.** Not per step, not per tool call — a
  question arriving mid-turn would be about a sentence the model has already
  half-answered.
- **Only what a person typed is matched.** A woken turn — a task landing, a job
  exiting, a watch with news — opens with an empty message and reads its note off
  the steering queue. Scoring that note would be the harness talking itself into
  work nobody asked for, so authored and wake messages are never matched.
- **It is silent when nobody is watching.** No `Config.Harnesses`, no
  `Config.RunHarness`, or no `Config.AskConsent` and none of this exists: not one
  extra branch a person can observe. A headless run, `--once` and a cron wake are
  never asked a question nobody will be shown.
- **The engine stays out of the conversation.** `session` decides *whether* a
  harness runs — the half a person answers — and `RunHarness` decides what
  running one means.

### The card — `internal/tui3/harness.go`

One row, under the approval question and the connect offer, the third rung of
the same lane:

```
? run harness "research"? · finds an answer across sources · [enter] run · [esc] no
```

`enter`/`y` run it, `esc`/`n` do not; both key chips are pointer targets. While
it is up the draft is suspended, as under the two blocks above it. The
description is dropped first when the frame is narrow — the answers are never
what gets cut — and the row dies with the turn that raised it. A yes is followed
by `EventHarnessRun` as a dim `harness · research` note and then the harness's
report as ordinary text; a no writes nothing anywhere, because a declined offer
is a thing that did not happen.

## The card — `internal/subharness/card.go`

A harness arrives as JSON a model wrote, and nobody approves JSON. `Card(Harness)`
is the one rendering of a shape, for every surface that shows one: the numbered
steps **in the order they run** (the same topological walk the runner takes), the
fields each kind actually uses on the right, the branch's arms and the split's
lanes labelled underneath, and the bounds in the foot — tools, verify rung,
dynamism rung and cap. `RunCard(Trace)` is its twin, deliberately the same
columns, because the question a person opens a trace with is "where did this
differ from the card". A cyclic program still draws: the card is what somebody
reads to find out a shape is wrong.

## Conditions — `internal/subharness/predicate.go`

The tiny language a `branch`'s `when` and a `loop.until`'s `until` may be written
in: `always`, `never`, `ok`, `failed`, `empty`, `nonempty`, and `contains`,
`equals`, `matches` with the rest of the line taken verbatim. It asks about ONE
thing — what the step before produced, and whether it succeeded (`State`).

It is **not compulsory**. A condition may also be a sentence ("the suite is
green"), which no small language will ever hold. `Runner.cond` is the one place
the two meet: a condition `ValidCondition` accepts is decided here,
deterministically, and everything else is handed to `Env.Cond` to judge. A
condition that does not parse and is not judged is false **and** an error, never
a quiet true.

## The runner — `internal/subharness/exec.go`

`Run` (run.go) owns the SHAPE of a run; `Env` owns what a node DOES — a worker's
turn, a tool call, a question put to a person, a check that passes or fails, and
the judgement on a condition in sentences. `Runner` is the join: it turns an
`Env` into the `Exec` the walk wants, so the session implements `Env` over the
belt and the consent lane it already has, a test implements it over a script, and
both get identical control flow, identical bounds and an identical trace.

Three things end a run, in three different words on `Trace.Status`: an ERROR
fails it, a PERSON at a `human.gate` **declines** or **intervenes** it (neither
comes back as an error), the CONTEXT cancels it. A `verify` that returns false is
none of those — it is what `failed` is for and what a `loop.until` loops on — but
a program that FINISHES on an unaddressed false fails.

A `subharness.call` runs the child through its own `Runner` at `Depth+1`, bounded
by `MaxCallDepth`; the child's trace goes to the child's own history (`Saver`) and
the parent's trail keeps the pointer, the version and the status. A child that was
declined stops the parent.

## Triggers and hosting — `internal/subharness/trigger.go`

A `trigger` is the only kind that faces outward. `source: hosted` is offered to a
`Source` — one method, `AddTrigger(Hosted)` — which mounts it as `/harness <name>`.
The command line is derived and never configurable, so two harnesses cannot claim
one line. **What the command enters at is the trigger's successor**, read off the
edges rather than out of a field that could disagree with them, and it must be an
`agent.loop`: what a person types is a sentence, and a sentence needs a reader.

`args` is the allowed-argument whitelist, comma-separated; an empty whitelist
grants nothing, and `Hosted.Accepts` is the one refusal sentence every source
uses. `Store.HostAll` is the boot path — every registered harness's head — and it
SKIPS a page it cannot use rather than letting one bad file take every other
harness's command off the surface.

## The surface — `internal/tui3/harnesspanel.go`

`/harness` is the list of what is registered: the mark, the name and the version
on the left, what it is for and what its history says on the right. It is the
overlay grammar `/connect` already uses. Enter prints the CARD into the
conversation — prose belongs in the transcript, not in a second overlay — with the
last run under it. A run in flight leads the task strip as a chip, and the chip's
door is this panel.

It is the sibling of the harness OFFER (`internal/tui3/harness.go`), not its
replacement: the offer is a question about THIS TURN, the panel is a list of what
is SAVED.
