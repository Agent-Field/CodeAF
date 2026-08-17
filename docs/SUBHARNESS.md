# Sub-harnesses — internal/subharness

A sub-harness is a SHAPE of work, kept: a named, versioned program over a fixed
registry of node kinds, built in conversation, approved on a card, and run again
by name. It is not the other thing this word means in this tree — exec's
subharness is the WORKER a leaf runs on (linear, swe, bare; cmd/aforge's
subharness.go), and that seam is untouched.

## Object model

One registry entry per harness, at `~/.aforge/harnesses/<name>.hjson`:

- **identity** — name, description, author, and an integer `version`. The
  version is a POINTER (v1, v2, …) written by the store on every accepted save,
  never by a builder.
- **program** — an ordered list of nodes, some of which nest.
- **bounds** — the tool whitelist (empty means NO tools), the verification rung
  this harness climbs to, and the dynamism rung with its integer `cap`. All
  three are ceilings a file may tighten and never loosen; `Clamp` settles the
  silent numbers and `Validate` refuses the rest, in sentences a builder can act
  on.
- **evidence** — tests, and the run history.

On disk:

```
harnesses/
  triage-flake.hjson              the entry, at whatever version is current
  triage-flake/
    version/v1.hjson              every version that was ever current
    version/v2.hjson
    run/20260816T142530Z.json     one trace DAG per execution
```

The format is HJSON's JSON subset: the package writes strict indented JSON and
reads a little more than it writes — `#`/`//`/`/* */` comments and trailing
commas — so a file somebody annotated by hand still loads.

## Node kinds (the registry, kinds.go)

Library in the binary, not generated code. A file arranges these and may not
introduce a tenth.

| kind | what it is | needs dynamism |
| --- | --- | --- |
| `agent.loop` | a worker with a brief, bounded in turns | fixed |
| `tool.call` | one tool, with fixed arguments | fixed |
| `human.gate` | stop and ask a person | fixed |
| `verify` | climb one rung of the ladder | fixed |
| `trigger` | what starts this when nobody does (first step only) | fixed |
| `branch` | one arm, chosen by a condition | branch |
| `loop.until` | repeat until it holds, bounded | branch |
| `parallel.split` | lanes at once, joined | width |
| `subharness.call` | run another harness here | recursive |

`parallel.join` is a tenth kind that is **never written in a program**: the
split carries its lanes, and the join is minted by the runner so that a trace
DAG has the diamond the program's shape implies.

Conditions (`branch when`, `loop.until until`) ask about the step before them
and are deliberately seven words, not an expression language: `always`,
`never`, `ok`, `failed`, `empty`, `nonempty`, `contains <text>`,
`equals <text>`, `matches <regexp>`.

## The two ladders

Verification and dynamism are ARGUMENTS, not modes, and both are comparable —
which is the whole of the enforcement.

```
accept < schema < invariants < loop < report < rederive < adversarial < human
fixed < branch < width < metaprompt < recursive < selfmod
```

A `verify` node may name a rung at or below the harness's own; a kind may appear
only if the harness was granted its dynamism rung or higher (`Dynamism.Allows`).
`cap` is that rung's integer bound — the lane count, the loop ceiling.

## Running

`Runner` owns the SHAPE of a run and nothing else; `Env` is what makes the nodes
do anything (internal/session's tools_harness.go implements it over the belt,
the provider client and the consent lane; a test implements it over a script).

Three things end a run, in three different words: an ERROR fails it, a PERSON at
a gate declines or **intervenes** it, the CONTEXT cancels it. A `verify` that
returns false is none of those — it is what `failed` is for and what a
`loop.until` loops on — but a program that FINISHES on an unaddressed false
fails.

Every run writes one trace DAG under `harnesses/<name>/run/<ts>.json`: one node
per step that ran, each naming the nodes it came after, with the round, the
lane, the gate's answer and the branch that was taken.

## The conversation's side (internal/session/tools_harness.go)

One belt tool, `harness`, off inside a task node (nobody to ask) and absent
without a registry:

```
kinds → preview → register → run
```

`preview` and `register` are two calls on purpose: the CARD is what a person
approves, and a model that could register in one call would write into somebody's
registry on its own judgement. `register`, `run` and `retire` all ask, through
the same consent lane every dangerous tool call uses — so a surface that can draw
a consent card can already answer them, and an unwatched session refuses rather
than hanging.

A `human.gate` inside a run is that same lane with a THIRD answer:
`escalate: true` offers *intervene* — stop, I'll take it from here — answered
through `Agent.ResolveHarnessGate`. A surface with two keys still works: yes
approves, no declines.

## The surface (internal/tui3/harness.go)

`/harness` is the list of what is registered, with each row's version and what
its history says; enter prints the CARD into the conversation, with the last run
under it. A run in flight leads the strip as a chip, and the chip's door is the
panel. The gate's third answer is `[i] I'll take it` on the consent card.
