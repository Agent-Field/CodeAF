# Sub-harnesses — subharness/ (design)

## Object model
A sub-harness is a durable registry entry (`~/.aforge/harnesses/<name>.hjson`) with: identity (name, desc, author, version int), program (DAG over node kinds), tool whitelist, verify (ladder arg: accept/schema/invariants/loop/report/rederive/adversarial/human), dynamism (fixed/branch/width/metaprompt/recursive/selfmod; integer cap), tests, run history (each run emits a trace dag JSON). Version is a pointer (v1, v2, ...).

## Node kinds (registry)
`agent.loop` (internal/session/loop orientated model+whitelist), `tool.call`, `parallel.split/join`, `branch`, `loop.until`, `human.gate`, `verify`, `subharness.call`, `trigger` (hosted/idle/watch/source.command). Library-in-binary, not generated code.

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
