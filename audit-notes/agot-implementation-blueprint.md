# AGoT / JIT adaptive-graph phase — implementation blueprint

Design only. No code was edited to produce this. Every file and line reference
below was read at `chat-v2` with the working tree as it stands on 2026-08-11.

Evidence base: `audit-notes/headless-regression-audit.md` §6 (spec durability),
§7 (proposed fixes), §8 (static instrumentation), §10 (post-fix battery and the
Top 3 bottlenecks).

---

## 0. What this phase is and is not

The battery in §10 established three things about the current engine, and this
blueprint is the answer to exactly those three:

1. **`GovernorMinInFlight = 3` is the de-facto concurrency ceiling.** Being
   fixed by a parallel agent this wave. **Taken as done.** It is a hard
   prerequisite for the wall-time claims of W5, W8 and W9 — a wider graph on a
   3-wide governor buys nothing, so those waves' wall-time numbers are only
   readable after W0 lands.
2. **Harness is chosen at sizing and corrected only by paying for a failed
   leaf** (`internal/plan/size.go:459-478` `sizeApply`, `:501-507`
   `recordGeneralist`; 8 `node_worker_changed` escalations, each after a full
   wasted attempt — V6 `migration_runner` 221 s, `report_command` 522 s then
   dead). → **W4**.
3. **Plans are flat (max depth 2 in every run of the battery, fixed and
   baseline alike) and structurally frozen at t=0** (`internal/plan/plan.go:274-400`
   builds once; the only mutators that ever ran were failure-driven —
   `internal/resident/overrun.go:125-217`, serial by construction at `:36-38`).
   → **W5** (depth) and **W7/W8** (structural adaptation).

### Binding constraints, restated as design law

These are the user's, treated as non-negotiable. Every wave below states how it
obeys each one that touches it.

| # | Law | Where it is enforced in this blueprint |
|---|---|---|
| L1 | Chat submits ONE task; headless owns all decomposition. | No wave adds a decomposition decision above `planSubtree`. W5 moves decomposition *later*, never earlier or outward. |
| L2 | Joint objective = wall-time × token-cost × quality. Every decision states its effect on all three. | Per-wave "Three axes" section; W6 makes it a prompt-level fact instead of a private one. |
| L3 | NO hardcoded examples or heuristics in prompts. General meta-prompts + per-harness registered descriptors only. Seam: `SubharnessInfo`/`RegisterSubharness` (`internal/exec/subharness.go:34-52`, `:98-108`). | Every prompt sketch below is written without a worked example. W3 and W4 put all harness-specific text behind the registry. The grep law (`subharness.go:199-203`) stays: `Contains\|HasPrefix\|regexp\|ToLower` must still yield only the one prose hit. |
| L4 | Per-harness task templates: each subharness declares the spec shape it wants; the orchestrator renders into it. `swe` currently discards `Contract` entirely (`internal/exec/swe.go:526-544` flattens to one CLI positional). | W3. |
| L5 | Specs are durable objects surviving retry and re-targeting. Today the retry path re-authors from failure context and loses filenames and acceptance checks (§6); contracts are not persisted at task scale — zero `plan_graph` events. | W1. |
| L6 | A POSITIVE done-criterion, structured and checkable, authored with the task, consumed as the stopping condition by every growth mechanism. Universal across harnesses — never coding-specific logic in the shared orchestrator. | W1 authors it; W2 makes it the gate; W7/W8 consume it. |
| L7 | Cache-first context shape: stable prefix first, per-node delta last, in every new call. | Stated per new call. `cached_tokens = 0` in every run of the battery — this is the single largest untouched token lever, so no new call may be prefix-unstable. |
| L8 | Dependency-event-driven dispatch. Stage is a planning concept, never a dispatch gate. | Already true in the store: `internal/store/query.go:445-461` `Ready` gates on `edges` only, and `Runner.Serve` wakes on `r.wake` the instant a landing opens the ready set. The remaining violation is in the *planner*, which injects stage-anchored edges — `anchorLateStarts` (`internal/plan/graph.go:699`, called at `plan.go:472`). → W9. |
| L9 | Reason as an AI-harness scheduler: the only real load is API load; the cost of a split is the new uncached context it needs; measured history drives width. Never human-PM time estimates. | W6 rewrites `agentPremise` (`internal/plan/plan.go:50-53`) and turns the already-rendered measured line (`cmd/aforge/selfknow.go:86-106`) from decoration into an instruction. |
| L10 | The planner may add a research-shaped task from its existing vocabulary when it lacks grounding. No new node kind; a dedicated research subharness is explicitly deferred. | W8, second half. |
| L11 | Container nodes staying non-executing is FINE. Clever minimal dependency edges beat nesting. | W5 makes the JIT-expanded parent a gathering node (the store already skips it — `internal/resident/runner.go:464-469`). W9 spends the saved effort on edges, not on depth. |

### One correction to the approved direction, before anything is built

Proposal item [3] asks to *hold dispatch of a result's newly-unblocked frontier
for one `reviseOn` pass*. **That hold already exists.** The chain, read end to
end:

- `reviseAfter` is called at `cmd/aforge/chat.go:901-907`, **inside** the leaf
  closure that returns `resident.ExecResult` (the closure begins around
  `chat.go:495` and `plans.lookup` at `:521` is its head).
- `internal/resident/runner.go:587` calls that closure via `executeGuarded`, and
  only settles the node afterwards (`Complete` at `:645`ff). So while the
  sentinel is thinking, the landed node is still `running` and **its own
  consumers are structurally unclaimable** — `store.Ready` requires every
  dependency to be `Done/Failed/Cancelled` (`query.go:457`).
- A *sibling* landing cannot slip past either: it must take the same per-job
  `locks.pass` (`chat.go:2611-2612`) inside its own pre-settlement window.

So the zero-verdict finding in §10 is **not an ordering bug**. It is an
information bug: the sentinel is shown `graph.stateBlock()` — titles, summaries,
inputs and lock state (`internal/plan/graph.go:914-944`) — and asked to name
"which result, which assumption, which node" (`internal/plan/revise.go`,
`revisePrompt`). It cannot name an assumption it was never shown, because
assumptions live in briefs and briefs are not in the block. **W7 is therefore
re-scoped**: feed it briefs and the criterion, and add a regression test that
pins the ordering guarantee so a future refactor cannot silently lose it. This
saves the wall-time cost a new hold would have added — a real win against L2.

---

## 1. Wave sequence at a glance

| wave | goal | touches `cmd/aforge/chat.go`? | depends on |
|---|---|---|---|
| **W0** | governor ceiling (parallel agent) | no | — |
| **W1** | durable spec object + positive done-criterion | **yes** (3 anchors) | — |
| **W2** | one growth governor: `growJob`, criterion as stopping condition | no | W1 |
| **W3** | per-harness task templates; `swe` stops discarding the contract | **yes** (1 anchor) | W1 |
| **W4** | harness chosen at claim time from landed evidence | **yes** (1 anchor) | W1, W3 |
| **W5** | JIT expansion at claim time; depth loop leaves `plan.Build` | **yes** (1 anchor) | W1, W2 |
| **W6** | joint-objective premise; measured history as instruction | no | — |
| **W7** | the sentinel actually lands verdicts | **yes** (1 anchor) | W1, W2 |
| **W8** | coverage audit + research-shaped task on a grounding gap | no | W1, W2 |
| **W9** | minimal edges: transitive reduction, specific splice edges | no | W5 |

### Scheduling around the other session's `chat.go`

Five waves touch `cmd/aforge/chat.go`. To keep the contention window small,
**every one of them lands its logic in a new file in `package main` and reduces
the `chat.go` diff to a call substitution at a named anchor.** New files:
`cmd/aforge/leafspec.go` (W1, W3), `cmd/aforge/reselect.go` (W4),
`cmd/aforge/jit.go` (W5). The five anchors, with what changes at each:

| anchor | current site | wave | edit |
|---|---|---|---|
| A1 | `chat.go:2963-2992` — the `Scale != ScaleProject` branch of `planSubtree` | W1 | journal a one-node `plan.Graph` (spec + criterion) instead of `plans.putContract`; two lines |
| A2 | `chat.go:1602-1610` `leafContract` | W1 | read `Spec.Method`, keep the `takeContract` fallback; three lines |
| A3 | `chat.go:703-744` the single `exec.Task` literal | W1, W3 | one new field `Spec: leafSpec(plans, planNode, node)`; one line per wave |
| A4 | `chat.go:521-600` the leaf head (post-`plans.lookup`, pre-`executorFor`) | W4 | `subharness = reselectWorker(...)`; one line |
| A5 | `chat.go:495-521` the leaf head (before `jobSpace`) / runner hook wiring near `chat.go:482` `NewRunner` | W5 | pass one `Expand` hook into `NewRunner`; one line |

W2, W6, W8 and W9 are `chat.go`-free and can land in any window, including
while the other session holds the file.

---

## W1 — The task spec becomes a durable object with a positive done-criterion

### Goal

Make the thing a worker is handed a **structured object authored once and
carried forward**, and give it a **positive, checkable stopping condition**.
Today the spec is prose authored per-scale (§6: `task`/`lookup` scale → the
compiler's goal + `Compiled.Contract`; `project` scale → plan brief + plan
contract), and the retry path re-authors it from failure context, losing the
module name, the filename, the constructor and the `go build` acceptance check
(§6, `task-2-n16`). Nothing consumes a criterion because none exists.

This wave adds no new model call. The criterion rides the calls already being
paid for.

### Files and functions

**New — `internal/plan/spec.go`**

```go
type Check struct {
    // Kind is "run" or "read". Universal across harnesses: a condition is
    // either settled by executing something and reading an outcome, or by
    // reading the artifact and finding something present.
    Kind   string `json:"kind"`
    Check  string `json:"check"`
    Expect string `json:"expect"`
}

type Done struct {
    // Produces names the identifiable outputs by the names they will carry.
    Produces   []string `json:"produces,omitempty"`
    Conditions []Check  `json:"conditions,omitempty"`
}

type Spec struct {
    Instruction string `json:"instruction"`          // was Node.Brief
    Method      string `json:"method,omitempty"`     // was Node.Contract
    Done        Done   `json:"done,omitempty"`
    Sources     []string `json:"sources,omitempty"`  // mirrors Node.Sources
}

func (s Spec) Render(limit int) string   // stable field order, bounded
func (s Spec) Empty() bool               // an empty Spec renders to "" — the
                                         // byte-identical fallback path
```

**Changed**

- `internal/plan/graph.go:65-155` `Node` — add `Spec Spec \`json:"spec,omitempty"\``.
  `Brief` and `Contract` stay and remain the source of truth for one release;
  `Spec.Instruction`/`Spec.Method` are populated beside them. Dual-write, single
  read, so rollback is a one-line read swap.
- `internal/plan/brief.go` `briefPrompt` (`:43-75`) and its schema — the call now
  returns instruction **and** criterion (below). `briefWriter.apply` writes both.
- `internal/plan/contract.go` `contractPrompt` — **unchanged text**; its result
  lands in `Spec.Method` as well as `Node.Contract`.
- `internal/resident/planadapter.go:130-155` `SubtreeFromPlan` — carry the spec
  into `store.NodeSpec`. `nodeBrief` (`:171`) unchanged.
- `internal/store/store.go:342-366` `NodeSpec` — add `Spec json.RawMessage \`json:"spec,omitempty"\``;
  `store.Node` (`:374+`) gains the same, read back in `query.go` alongside
  `Subharness`. Journal-derived like every other node field.
- `internal/store/planjournal.go` — no change to the type; the change is that
  **task-scale jobs now journal one** (see A1).
- `internal/resident/overrun.go` `OverrunGoal` (`:57-88`) — the original spec
  travels with the remainder instead of being re-derived from `node.Brief`.
- `internal/resident/revise.go` `ApplyRevision` (`:37-70`, the `add` case) —
  `spec.Spec` is set from the plan node, so a sentinel-added node is a spec
  like any other.
- `internal/revision/judge.go` — the replacement node authored on the retry path
  inherits the failed node's `Spec.Done` verbatim and may only rewrite
  `Instruction`/`Method`. This is the §6 defect, closed structurally.
- **`cmd/aforge/leafspec.go` (new)** — `leafSpec(plans, planNode, node) plan.Spec`,
  the one reader; anchors A1/A2/A3 call into it.

### What the prompts say

**Added to `briefPrompt`, as its own trailing block** (general, no example, no
domain vocabulary):

> Alongside the instruction, state the criterion by which this work will be
> judged finished. It is a positive statement of what must be true once the work
> has landed — not a list of steps, and not the instruction said again.
>
> Give it as a small set of independent conditions. Each one must be settleable
> by someone who has the result in front of them and did not do the work.
> A condition that can be settled by running something says the exact thing to
> run and what its outcome must be. A condition that can only be settled by
> reading says what must be present and what would make it absent.
>
> Name the things the result must produce, by the names they will carry, so that
> a reader holding only this criterion could tell whether they exist.
>
> Write no condition the request did not ask for. A criterion that demands more
> than the person asked is a criterion that cannot be met, and every condition
> you invent becomes a requirement nobody made.

Schema becomes
`{"instruction": string, "done": {"produces": [string], "conditions": [{"kind": "run"|"read", "check": string, "expect": string}]}}`.

**Added to `OverrunGoal` and to the retry authoring path**, one sentence:

> The criterion this work is judged against has not changed and is given below
> unaltered. Plan against it; do not restate it, extend it, or replace it.

**Nothing coding-specific appears anywhere.** `"run"` versus `"read"` is the
only structural distinction and it is universal: a prose deliverable's
conditions are `read` conditions, a buildable one's are `run` conditions, and the
shared orchestrator never inspects which.

**Cache shape (L7):** unchanged prefixes everywhere. The criterion is *output*
on an existing call, so zero new prompt tokens and zero new prefix churn. The
spec's own render (`Spec.Render`) puts `Method` first (stable per job) and
`Done` last, because `Done` is what a retry rewrites least and an instruction
rewrites most — the ordering matters in W3, where this string reaches a foreign
engine's own cache.

### How it is tested

- `internal/plan/spec_test.go` — round-trip; `Spec.Empty()` renders to the empty
  string and the whole path is byte-identical to today (this is the rollback
  proof).
- `internal/plan/brief_test.go` — the new schema decodes; a response with no
  `done` key yields today's brief unchanged.
- `internal/store/splice_test.go` — spec survives splice → journal → rebuild.
- `internal/resident/overrun_test.go` — a replanned remainder carries the
  original `Done.Produces` and `Done.Conditions` verbatim.
- `internal/revision/judge_test.go` — a replacement node for a failed leaf has
  the same `Done` as the node it replaces. **This is the §6 regression test.**
- `cmd/aforge/chat_test.go` — a `ScaleTask` job journals exactly one
  `plan_graph` event (today: zero).
- Pod: **V1** and **V5**. Query
  `SELECT COUNT(*) FROM events WHERE kind='plan_graph'` — must be ≥ 1 for every
  job including task-scale. On V1, diff the failed node's spec against its
  replacement's: `Done` must be identical.

### Three axes

- **Wall time:** neutral. No new call. Rendering cost is microseconds.
- **Token cost:** +3-6 % completion tokens on `brief` calls only (the criterion
  is 40-80 tokens); **0** new prompt tokens; **0** prefix churn. On the retry
  path it is a net *saving*: the replacement no longer re-derives what already
  existed.
- **Quality:** the largest single-wave gain available. §6 measured the retry
  path dropping the module name, the filename, the type names and the acceptance
  check; a spec that survives re-targeting cannot drop them. Also the
  precondition for W2's positive stopping condition — without it, growth can only
  ever be bounded negatively.

### Risk / rollback

Risk: a weaker model returns a malformed or over-reaching `done` block, and the
criterion becomes a source of invented requirements. Mitigations: (a) the schema
is closed and the conditions array is capped at a small count; (b) an absent or
empty `done` is legal and produces today's behaviour byte for byte; (c) the
"write no condition the request did not ask for" clause mirrors the language
already load-bearing in `contractPrompt` ("every demand you write becomes a
requirement the person never made").

Rollback: `plan.Options.Criterion bool`, default on; off restores the previous
brief schema and makes `Spec.Empty()` true everywhere, which every downstream
reader already handles.

**chat.go anchors: A1, A2, A3.**

---

## W2 — One growth governor: `growJob`, with the criterion as the stopping condition

### Goal

Every execution-time add goes through one helper carrying the ceiling, the
per-job growth-round counter, and a **positive** gate: *is the goal already
satisfied once in-flight work lands?* Today growth is bounded only negatively
and inconsistently:

- `internal/resident/overrun.go:35-49` holds `MaxOverrunRounds = 3` and
  `maxJobNodes = 90`, checked at `:129-133` and `:157-161`.
- `internal/revision/judge.go:503-531` `ExtendForGap` inherits those by calling
  `ReplanOverrun`.
- **`internal/resident/revise.go:21-135` `ApplyRevision` bypasses all of them.**
  Its `add` case splices straight into the job root with no ceiling, no round
  counter, and no rail check. That is the hole to close, and it is exactly the
  mechanism W7 is about to start firing.

### Files and functions

**New — `internal/resident/grow.go`**

```go
type GrowRequest struct {
    JobRoot   string
    Reason    string          // "overrun" | "gap" | "revision" | "jit" | "coverage"
    Adding    int             // node count the caller is about to splice
    Criterion plan.Done       // the job's, from W1
    Landed    []store.DependencyInput
    InFlight  []plan.Spec     // what running/pending work is committed to produce
}

type GrowVerdict struct {
    Allow   bool
    Round   int
    Refused string           // human words, posted through postGovernorNotice
}

func growJob(ctx context.Context, graph *store.Store, ask Satisfier, req GrowRequest) (GrowVerdict, error)
```

Order of checks, cheapest first:

1. **Round cap** — one counter per job root, derived from journalled growth
   events (not from id suffix parsing, which is `-x` lineage and is per-leaf).
2. **Node ceiling** — `NodeIDsWithPrefix(jobRoot) + Adding > maxJobNodes`.
3. **Daily rail** — `PauseDailyRail`, as `replanOverrun:135-150` does now.
4. **Satisfaction gate** — the model call below. Asked *last*, so it is only
   paid for when the cheap refusals have all passed.

**Changed**

- `internal/resident/overrun.go` — `MaxOverrunRounds`, `maxJobNodes` move to
  `grow.go`; `replanOverrun:125-165` replaces its two inline checks with one
  `growJob` call. Behaviour identical when the satisfaction gate is disabled.
- `internal/resident/revise.go:37-70` — the `add` case calls `growJob` first;
  a refusal becomes a note, exactly as a store rejection already does.
- `internal/revision/judge.go:503-531` — `ExtendForGap` passes its `Reason`
  through instead of relying on `ReplanOverrun`'s inherited caps.
- `internal/store/store.go:86-110` — new `EventKind` `job_growth`, payload
  `{reason, adding, round, allowed, refused}`. This is the observability the
  battery lacked: §10 could not tell an overrun replan from a sentinel add
  without reading intents.
- **New — `internal/plan/satisfied.go`** — `Satisfied(ctx, client, criterion, landed, inflight) (Verdict, Usage, error)`.

### What the prompt says

`satisfiedPrompt` (general; no examples; no domain vocabulary; consumed
identically by prose and code jobs):

> You decide whether a job still needs work added to it.
>
> You are given the criterion the finished job is judged against, what has
> already landed, and what is still in flight together with what each piece in
> flight is committed to producing. Assume every piece in flight lands exactly as
> committed.
>
> Under that assumption, take each condition of the criterion in turn and ask:
> is it already met, or will it be met by something in flight? A condition that
> would merely be better served is met. Being richer is not the test. Being met
> is.
>
> Answer that the job is complete when every condition is covered. For each
> condition you cannot cover, name the condition and say what is missing from
> it — the thing no landed and no in-flight work produces.
>
> Do not propose work. Naming the gap is the whole answer. Do not invent
> conditions the criterion does not contain, and do not treat verification of
> work already done as a gap: a condition met by a result is met, and checking
> it again is not coverage, it is a second job.

Schema: `{"complete": bool, "uncovered": [{"condition": string, "missing": string}]}`.

That last paragraph is the 27-round-spiral doctrine (`overrun.go:29-40`,
`revise.go` `checkingRule`) restated where the new gate can enforce it, which is
what the existing comments say a cap alone cannot do.

**Cache shape (L7):** the message order is (1) static system prompt, (2) the
job criterion — fixed for the job's lifetime, (3) the landed-results table in
`created_seq` order — **append-only**, so the prefix only ever grows at the
tail, (4) the in-flight commitments and the triggering event, last. This is the
one new recurring call in the design and it is deliberately the most
cache-friendly shape available.

### How it is tested

- `internal/resident/grow_test.go` — every caller (`overrun`, `gap`, `revision`,
  `jit`, `coverage`) is refused past the ceiling; **`ApplyRevision` specifically**
  is refused, which today it is not.
- Round counter is per job, not per leaf lineage; two siblings each splitting
  once do not consume each other's rounds.
- A job whose criterion is fully covered refuses growth even with rounds and
  nodes to spare, and posts words a person can read.
- Gate disabled → every existing `overrun_test.go` / `judge_test.go` passes
  unchanged (this is the rollback proof).
- Pod: **V5** (mid-run adaptation) and **V6** (24-node sprawl). Query
  `SELECT reason, allowed, round FROM events WHERE kind='job_growth'`; assert no
  job's node count ever exceeds `maxJobNodes`, and that at least one refusal
  cites coverage rather than a cap.

### Three axes

- **Wall time: down.** The measured cost of ungated growth is V4's two serial
  overrun rounds — +171 s of tail with nothing else running, and V5's ratio of
  **0.77**. A satisfaction gate that refuses a round costs one small call
  (~2-4 s) and saves a full replan plus a full leaf.
- **Token cost: down.** The gate is a few hundred prompt tokens against a
  refused round's full replan (a fan-out plus a size pass) plus the leaf it
  would have spawned. It is asked last, after three free checks, so on the
  common path it is never asked at all.
- **Quality: up, with one risk.** Up: it stops the "invented verification of the
  round before it" spiral the comments in `overrun.go:29-40` describe. Risk: a
  gate that wrongly says "complete" truncates genuinely unfinished work — which
  is why the prompt's default direction is *uncovered*, and why the gate runs
  after, never instead of, the cheap caps.

### Risk / rollback

Rollback: `AFORGE_GROWTH_GATE=0` (or a `resident.Options` field) skips step 4
and leaves steps 1-3, i.e. today's behaviour plus the `ApplyRevision` fix and the
journal. The `ApplyRevision` fix is worth landing on its own even with the gate
off.

**chat.go anchors: none.**

---

## W3 — Per-harness task templates; `swe` stops discarding the contract

### Goal

Each subharness declares the spec shape it wants; the orchestrator renders the
brief, method and criterion into the *receiving* harness's template. Today the
`exec.Task` is one shape for everyone (`internal/exec/executor.go:46-123`),
populated once at `cmd/aforge/chat.go:703-744`, and `swe` reads roughly half of
it: `Contract` is **never read**, and everything is flattened into one CLI
positional at `internal/exec/swe.go:526-544`
(`argv = append(argv, "--max-cost", …, "--", goal)`). A per-leaf model call
writes a contract (`internal/plan/contract.go`), it is paid for, and for coding
leaves it is thrown away — while `internal/exec/linear.go:332-336` describes that
same contract as *"what a specialised harness would have hand-written for this
domain"*.

### Files and functions

- `internal/exec/subharness.go:34-52` `SubharnessInfo` — two new fields:

  ```go
  // Template renders one task into the shape this worker wants to receive.
  // Nil renders exactly what the worker received before templates existed,
  // so a registration that says nothing about shape is served, not refused —
  // the same contract DeadlineFloor already has.
  Template func(Task) string

  // Accepts declares which channels this worker can actually consume, so the
  // composer stops paying for what will be dropped.
  Accepts InputCapabilities   // {Method, Criterion, Images, Documents, Board, Reflex bool}
  ```

- `internal/exec/subharness.go:98-108` `RegisterSubharness` — passes both
  through; `plan.UseSubharness` gains nothing (the plan package must not learn
  about templates).
- `internal/exec/executor.go:46-123` `Task` — add `Spec plan.Spec` (W1's
  object). `Brief`/`Contract` stay; the template reads whichever it wants.
- `internal/exec/swe.go:526-544` `argv` and `:688-710` `sweGoal` — `sweGoal`
  becomes the registered template and now composes **invariants → method →
  instruction → criterion**, in that order.
- `internal/exec/linear.go:332-345` — same seam, same order; today's system
  message is the linear template, so this is a refactor with a byte-identity
  test.
- `internal/exec/subharness_swe.go:30-42` — registers the template and a
  spec-shape sentence appended to `Purpose` (see below).
- `internal/exec/schedule.go:506-545` `taskFor` — the `aforge run <graph.json>`
  path composes a strictly poorer Task (§8.1); route it through the same
  composer so there is one spec author, not two.
- `cmd/aforge/leafspec.go` — `Task.Spec` populated here; **A3** is one line.

### What the prompts say

No new model call. The template is a *rendering* contract.

The one prompt-visible change is a sentence appended to each specialist's
registered `Purpose`, which is already read by all four selection sites
(§8.3) — general in form, harness-authored in content, and therefore compliant
with L3:

> …and this is what a good assignment for it looks like: <the harness's own
> declared spec shape, one sentence, registry-authored>.

The shared orchestrator never reads that string; it only forwards it.

**Cache shape (L7):** the ordering **invariants → method → instruction →
criterion** is chosen for the receiving engine's own prefix cache. Invariants
are process-constant, the method is per-leaf but written once and never
rewritten, the instruction is what a retry rewrites, and the criterion is
appended last so a re-targeted retry changes only the tail. For `swe` today
everything is one uncached positional; this is what makes it cacheable at all
should the engine ever cache it.

### How it is tested

- `internal/exec/swe_test.go` — the rendered goal contains the method and the
  criterion; a task with an empty `Spec` renders **byte-identically** to today's
  `sweGoal` (rollback proof).
- `internal/exec/subharness_test.go` — a registration with no `Template` gets
  today's composition; `Accepts` false for a channel means the composer does not
  pay to build it (measurable: `DocumentPaths` staging skipped).
- The grep law (`subharness.go:199-203`) re-asserted: no new by-name `swe`
  reference outside the executor, the constructor table and the registration.
- Pod: **V1** (single coding leaf that "never converged") and **V2** (7 coding
  leaves). Metric: `go build ./... && go test ./...` on the produced workspace,
  and V1's convergence.

### Three axes

- **Wall time: down** on coding leaves. V1 ran 893.5 s and never converged; the
  contract's "how to verify" and "where this gets stuck" paragraphs are exactly
  the content that shortens a grind. Expect the effect where §10 measured the
  loss, not elsewhere.
- **Token cost: mixed, small.** The `swe` positional grows by the method
  (120-200 words) and the criterion (40-80). Against V2's 1,057,369 prompt
  tokens that is noise; against V1's 238,602 for a leaf that never converged it
  is the cheapest possible intervention. Ordering it invariants-first is what
  keeps it from being re-sent uncached forever.
- **Quality: up, and this is the wave's whole point.** §8.1 measured a paid-for
  artifact being discarded for every `swe` leaf. Section 10's V1 defect
  ("compiles, tests FAIL, never converged") is precisely a missing verify loop.

### Risk / rollback

Risk: a longer CLI positional hits an engine argument limit, or the engine
parses the composed text worse than the terse version. Mitigation: the template
is per-harness and can cap its own render (`Spec.Render(limit)`); the fallback is
`Template == nil`, which is today.

Rollback: unregister the template — one line in `subharness_swe.go`.

**chat.go anchor: A3.**

---

## W4 — Harness chosen at claim time from landed evidence, not at sizing

### Goal

Bottleneck #2. The sizing pass names a worker before any dependency has landed
(`internal/plan/size.go:459-478`), and the only correction is a full failed leaf
followed by `node_worker_changed` — 8 times in the battery, V6's
`migration_runner` burning 221 s and `report_command` 522 s before escalating and
then dying anyway. The reverse error is equally live: V3's explicitly-code
deliverable was sized entirely generalist and the generalist shipped a building,
passing Go package. **The sizing verdict is a prior, not a decision**, and this
wave says so structurally.

### Files and functions

- `internal/plan/size.go:459-478` `sizeApply` — the specialist branch keeps
  writing `node.Subharness`, but the field is documented and journalled as a
  *prior*. `:501-507` `recordGeneralist` keeps its `"" vs "linear"` distinction
  (the F1b fix), which is what makes "nobody asked" distinguishable from "the
  generalist, chosen" — the re-selection needs exactly that distinction.
- **New — `internal/exec/reselect.go`** —
  `ChooseWorker(ctx, client, spec plan.Spec, landed []store.DependencyInput, prior string) (string, Usage, error)`,
  schema `enum: ["", <registered names>]` generated from the registry exactly as
  `sizeSchemaFor` does (`size.go:321-355`), so a worker this process does not
  have cannot be named.
- `internal/resident/runner.go:337-400` `dispatchOne` — an optional
  `Reselect func(store.Node) string` hook, consulted after the claim and before
  the node is handed to the worker. Nil hook = today.
- **New — `cmd/aforge/reselect.go`** — the policy that bounds the cost:

  > Re-ask only when the claim can see something the sizing pass could not:
  > (a) the node has at least one dependency whose digest landed after the plan
  > was written, or (b) the node carries no verdict at all (`Subharness == ""`),
  > or (c) the node is a JIT child (W5), which was never sized against the
  > top-level ruler. Otherwise the prior stands and nothing is spent.

- `internal/store/splice.go:336-338` — unchanged; inheritance still applies only
  to a genuinely empty verdict.

### What the prompt says

`reselectPrompt` (general; menu and measured lines come from the registry, no
examples, no keyword matching anywhere in the path):

> You choose which worker takes one piece of work, at the moment it is about to
> start.
>
> You are given what it must deliver, the criterion it will be judged against,
> what its dependencies actually produced, and — for each worker available —
> what that worker is for and what work of this kind has really cost on this
> machine.
>
> Choose the worker whose purpose matches what this piece of work is, in its
> essence. Measured history is evidence about cost, not about fit: a worker that
> has lately finished this kind of work cheaply is worth preferring among workers
> whose purpose already matches, and is never a reason to hand work to a worker
> whose purpose does not.
>
> An earlier judgement was made about this before any of its inputs existed, and
> you are told what it was. Depart from it only when what actually landed says
> something that judgement could not have known. Agreeing with it is a complete
> answer.
>
> When no available worker's purpose matches, leave it unset; the baseline worker
> takes it. You are not being asked whether the work is hard. You are being asked
> what kind of work it is.

**Cache shape (L7):** (1) static system prompt, (2) the registry menu plus the
measured lines — process-stable, identical for every leaf in the run, (3) the
prior, (4) the spec and the landed digests, last. Leaf-to-leaf, only the tail
changes, so this call is close to fully prefix-cacheable within a run.

### How it is tested

- `internal/exec/reselect_test.go` — the enum is registry-generated; an
  unregistered name cannot come back; an empty answer means the baseline.
- `cmd/aforge/reselect_test.go` — the policy: a node with no landed dependency
  and a recorded verdict is **not** re-asked (cost bound); a JIT child always is.
- `internal/resident/runner_test.go` — nil hook is byte-identical to today.
- Pod: **V3** (per-node harness) and **V6** (24 nodes, 5 escalations). Metric:
  `SELECT COUNT(*) FROM events WHERE kind='node_worker_changed'` should fall
  toward zero, and the seconds burned before each escalation (V6: 221 s + 522 s)
  should not recur.

### Three axes

- **Wall time: down, measurably.** Each avoided escalation removes a full
  leaf-length attempt. V6 alone spent 743 s on two of them.
- **Token cost: down.** One small call (a few hundred prompt tokens, mostly
  cached) against a wasted full leaf — V6's failed attempts are in the tens of
  thousands of tokens each.
- **Quality: up in both directions.** The discriminator is weak both ways (§10);
  a decision made when the dependency digests exist is made with strictly more
  information than the one made at t=0.

### Risk / rollback

Risk: a per-leaf call on a wide graph is a per-leaf cost, and a re-selection that
flip-flops against the plan's own sizing produces a node sized for one worker and
run by another (an oversized generalist node handed to a specialist is fine; the
reverse is not). Mitigations: the "agreeing is a complete answer" clause; the
re-ask policy that skips the common case; and the size verdict travels in the
prompt so a departure is deliberate.

Rollback: nil the hook — one line at A4.

**chat.go anchor: A4.**

---

## W5 — JIT expansion at claim time; the depth loop leaves `plan.Build`

### Goal

Bottleneck #3, first half. `internal/plan/plan.go:486-505` runs the depth loop
inside the build:

```go
for level := 0; level < options.MaxDepth; level++ {
    spliced, expandUsage, expandErr := ExpandLevel(ctx, client, graph, options)
```

so every decomposition decision is made at t=0 against **titles**, and the
measured result is a parent-depth histogram of `{0:1, 1:1, 2:N}` in **every
single run of the battery, fixed and baseline alike**. Move the loop to the
scheduler: an oversized node is expanded **at claim time**, when its
dependencies have actually landed and their digests can be read.

The machinery is already the right shape. `expandOne`
(`internal/plan/expand.go:169-228`) takes **one node and costs two calls** (a
fan-out and a size pass; deliberately flat — see its own comment on why a full
staged build inside a node turned a critical path of 3 into 12).
`selectForExpansion` (`:122-167`) is a list-returning budget allocator that is
already a per-node predicate wearing a loop.

### Files and functions

- `internal/plan/expand.go:122-167` — extract
  `ShouldExpand(node Node, options Options, remaining int) bool` (the
  `Kind/Frozen/Depth/len(Parts) < 2/Size` predicate, verbatim). `selectForExpansion`
  becomes a thin loop over it so `ExpandLevel` is unchanged and every existing
  test holds.
- `internal/plan/expand.go:169` — export `ExpandOne`, and give `expandScope`
  (`:19-44`) two new fields: `Landed []store.DependencyInput`-shaped digests and
  `Criterion plan.Done`. `worthKeeping` (`:263`) unchanged — the shrinkage guard
  is exactly as needed at claim time.
- `internal/plan/plan.go:486-505` — the loop becomes conditional on a new
  `Options.ExpandAtBuild bool`. **The resident path sets it false; `aforge run`
  and any test that wants a fully-built graph sets it true.** This is the whole
  behavioural switch and the whole rollback.
- `internal/resident/runner.go:337-400` `dispatchOne` — an optional
  `Expand func(store.Node) (spliced int, ok bool)` hook, consulted **after** the
  claim and **before** the worker. On `ok`, the runner releases the claim
  (`graph.Release(claimed)` — the path already exists on the fault branch) and
  returns; the next tick finds the children ready and the parent skipped by the
  open-children rule at `:464-469`.
- **New — `internal/resident/jit.go`** — reads the job's plan graph
  (`store.PlanGraphFor`), finds the plan node, calls `plan.ShouldExpand`, then
  `plan.ExpandOne`, then `growJob` (W2), then
  `planadapter.SubtreeFromPlan` + `store.Splice` under the claimed node.
- **New — `cmd/aforge/jit.go`** — wires the hook with the job's plan client;
  **A5** is one line at `NewRunner` (`chat.go:482`).

**On containers (L11):** the expanded parent stays in the store as a
non-executing gathering node. That is already how the store behaves — the
comment at `runner.go:464-469` says *"A goal node lands after its children"* —
and it is the user's explicit decision. No nesting effort is spent; W9 spends it
on edges instead.

### What the prompt says

`expandScope.render` (`expand.go:26-44`) gains two blocks. The existing
`"It will receive the results of:"` line — which today lists **titles** — is
replaced when digests are available:

> The work below has already finished, and this is what each piece actually
> produced. Break this piece down against what is really there, not against what
> was expected of it before anything ran.

and, immediately before `"Break down only this piece of it:"`:

> When this piece is finished, this must be true of it: <criterion>. Every part
> you name has to contribute to that, and the parts together have to cover all
> of it — no more and no less.

Both are general. Neither mentions files, code, documents or any domain.

**Cache shape (L7):** `expandOne` already pins `provider.ClassPlanExpand` and
inherits `graph.Settled/Open/Evidence/Terrain` verbatim (its comment explains
why: a sub-planner that rebinds free variables produces Berlin, Paris and Madrid).
The render order becomes (1) goal + settled points + terrain — identical for
every expansion in the job, (2) ancestry and siblings — stable per parent, (3)
landed digests and criterion — the delta, last. Today the digests would have
been in the middle; putting them last is the whole L7 compliance of this wave.

### How it is tested

- `internal/plan/expand_test.go` — `ShouldExpand` returns exactly what
  `selectForExpansion` selected, node for node, on every existing fixture.
- `internal/plan/plan_test.go` — `ExpandAtBuild=true` produces byte-identical
  graphs to today (rollback proof).
- `internal/resident/jit_test.go` — a claimed oversized node with landed
  dependencies splices children and releases its claim; the parent becomes
  non-ready until its children are terminal; a frozen or already-running node is
  never expanded; a `growJob` refusal leaves the node to run whole (which is what
  a failed expansion already means, per `ExpandLevel`'s own contract).
- Pod: **V6** (the 24-node case that never nested) and **V2**. Metrics:
  parent-depth histogram max ≥ 3; ratio (sum of leaf seconds ÷ leaf span) up
  from V6's 2.27; and — the one that matters — **zero nodes decomposed only after
  exhausting a budget**, cross-checked against `job_growth` reasons.

### Three axes

- **Wall time: down, conditional on W0.** V6's completed leaves summed 2,604 s
  into a 1,147 s span with 13 nodes pending behind a 3-wide governor. Splitting a
  513 s leaf into four helps only once the fan can exceed 3. With W0, this is the
  single largest wall-time lever in the design.
- **Token cost: up on the planning side, down on the execution side.** Each
  claim-time expansion is **two calls** (`expandOne`'s fan-out + size), against
  today's zero-at-claim and one-full-wasted-leaf-then-overrun-replan. The overrun
  path costs a full leaf budget plus a replan; this costs two small calls. The
  `worthKeeping` shrinkage guard (`:263-289`) already refuses splits that buy
  nothing — one earlier run burned 17,000 output tokens on splits that were all
  rejected, and the `len(Parts) < 2` pre-check exists because of it. Net expected:
  down, but this is the wave whose token effect most needs measuring rather than
  asserting.
- **Quality: up.** A node decomposed against real dependency digests cannot
  invent the shape of an input. §10's V5 showed content flowing correctly through
  edges while shape never changed; this is where shape starts following content.

### Risk / rollback

Risks, in order of seriousness:

1. **Runaway depth.** Bounded three ways: `options.MaxDepth` still applies per
   node (`ShouldExpand` reads `node.Depth`), `growJob`'s round counter is
   per-job, and `worthKeeping` refuses non-shrinking splits.
2. **Claim churn** — a node claimed, released, and re-claimed each tick.
   Prevented because expansion mutates the node into a container; the
   open-children rule then makes it unclaimable, and the expansion is journalled
   so a restart does not repeat it.
3. **Latency at the head of a leaf.** Two calls (~4-10 s) before the first leaf
   of an expanded node starts. Bounded by the re-ask policy: only nodes
   `ShouldExpand` already selects — `Oversized` or `Borderline` with two named
   parts — ever pay it, which in the battery is a small minority.

Rollback: `Options.ExpandAtBuild = true` plus a nil hook restores today exactly.

**chat.go anchor: A5.**

---

## W6 — The joint objective becomes a fact the prompts know

### Goal

§8.4's verdict: *every prompt optimizes one axis in isolation.* The load-bearing
string is `agentPremise` (`internal/plan/plan.go:50-53`), shared verbatim by
every planning prompt:

> "The work is done by AI agents. **They are instant, free, and unlimited in
> number.** They start together, never talk to each other, and never see each
> other's work."

Meanwhile the measured line already reaches all four selection sites
(`cmd/aforge/selfknow.go:86-106`) —
`median 40000 tokens, 12 turns over 31 runs; 90% succeeded; avg cost $0.0412` —
as *decoration the model is never told what to do with*. And
`internal/head/compiler.go:45,50,51` debits only lateness: *"the time the person
waits is the longest chain, never the total"*.

This is a prompt-only wave and it is cheap, which is why it is sequenced where a
`chat.go`-free slot exists.

### Files and functions

- `internal/plan/plan.go:50-53` `agentPremise` — one string, reaching every
  planning prompt (`brief.go`, `contract.go`, `audit.go`, `revise.go`,
  `size.go`, `fanout.go`, `bind.go`, `ensemble.go`).
- `internal/plan/plan.go:118-120` `proportionRule` — unchanged; it is already the
  only counterweight and it stays a burden-of-proof rule.
- `internal/head/compiler.go:45-55` — the width rule.
- **Golden test moves:** `internal/head/testdata/compiler_prompt_baseline.golden`.
  Flag this in the commit; it is the file most likely to collide with another
  session's edits to `head`.
- `internal/plan/size.go:239-242` and `:292` — the two places that already put
  two costs side by side (both currently latency).

### What the prompts say

`agentPremise` becomes (same length, same register, no numbers, no examples):

> The work is done by AI agents. They can be started in any number and they
> start together; they never talk to each other and never see each other's work.
>
> What they cost is not time. It is the context each one must be given, and a
> piece of work split in two costs whatever the second piece must be told that
> the first was already told. A split that shares almost everything is nearly
> free; a split that has to re-explain the whole subject twice is not. What the
> person waits for is the longest chain, never the total — so width is worth
> buying, and worth buying only where the pieces genuinely do not need the same
> thing said twice.

One sentence added wherever the measured line is rendered
(`selfknow.go:86-106`), turning decoration into instruction:

> The figures beside each worker are what work of this kind has really cost
> here. Read them as evidence about this machine, not as a target: prefer the
> worker whose purpose fits, and among workers that fit, prefer the one the
> evidence says finishes this kind of work.

**Cache shape (L7):** `agentPremise` is a system-prompt constant shared by every
planning call, so its edit is a one-time cache invalidation and nothing more.
The measured line is already appended **last** in the compiler's user message
(`compiler.go:342-344`), explicitly for cache stability — that placement stays.

### How it is tested

- Golden update for `compiler_prompt_baseline.golden`, reviewed as a diff.
- `internal/plan/*_test.go` — every prompt-composition test that embeds
  `agentPremise` moves together; a grep asserts exactly one definition.
- Pod: **V4** (the case where output was identical and the fixed arm cost +56 %
  spend and +55 % wall) and **V6**. Metric: node count and edge count for the
  same deliverable, and spend at equal output. V4 is the honest test because its
  two arms produced the same five correct briefs.

### Three axes

- **Wall time: neutral to slightly up.** A planner that stops believing agents
  are free will sometimes choose one node where it chose three. That is the trade
  being asked for, and V4 says it is worth making: 5 correct briefs for $0.0627 /
  246 s beat 5 correct briefs for $0.0978 / 382 s.
- **Token cost: down.** This is the wave aimed squarely at cost. Prompt-to-completion
  ratios of 29:1 (V4) and 16:1 (V6) mean the bill is context, and this is the one
  string that tells every planner context is free.
- **Quality: neutral, watched.** The risk is under-decomposition. `proportionRule`
  is a burden-of-proof rule in the other direction and stays; W8's coverage audit
  is the backstop that catches a plan that got too small.

### Risk / rollback

Risk: the single highest-blast-radius edit in the design — one string reaching
every planning prompt, and every plan shape in the system moves with it. Mitigation:
land it alone, in its own commit, with V4 and V6 run on both sides.

Rollback: revert one constant and one golden.

**chat.go anchors: none.**

---

## W7 — The sentinel actually lands its verdicts

### Goal

Across eight runs the revision sentinel produced **no `add`, `remove`, `rewire`
or `retitle` operation**. Every mid-run `plan_graph` event was an overrun replan.
As established in §0 above, **the ordering guarantee is already there** — the
work is to give the sentinel what it needs to answer, and to pin the ordering so
it cannot be lost.

### Files and functions

- `internal/plan/graph.go:914-960` `stateBlock` — add a sibling,
  `frontierBlock(frontier []int, budget int) string`, which renders the
  **unstarted frontier's briefs**, bounded, rather than titles. `stateBlock`
  itself is untouched — it is still the right render for the hierarchy, and the
  two go in different messages for cache reasons.
- `internal/plan/revise.go:128-172` `Revise` — signature gains the frontier and
  the job criterion. Message order changes (below). `apply` (`:179-248`)
  unchanged: the frozen rule and the refusal recording stay exactly as they are.
- `internal/plan/revise.go` `revisePrompt` — two added paragraphs, one changed
  sentence (below).
- `cmd/aforge/chat.go:2594-2631` `reviseOn` — computes the frontier (the pending
  nodes of this job with no unsettled dependency other than the one that just
  landed) and passes it down. **The pending-count loop at `:2599-2610` already
  walks exactly the right node set**, so this is a widening of an existing loop,
  not a new query. `internal/revision.Sentinel` forwards it.
- `internal/resident/revise.go:37-70` `ApplyRevision` — every `add` goes through
  `growJob` (already landed in W2); nothing else changes.
- **New test file** `cmd/aforge/revise_order_test.go` — the ordering guarantee.

### What the prompt says

Added to `revisePrompt`, after the `[locked]` paragraph:

> The nodes below have not started, and one of them is about to. Their
> instructions are given in full rather than by name, because an instruction is
> the only thing that can hold an assumption — a title cannot contradict a
> result and a sentence of work can. Read each instruction against what has just
> happened and ask whether what that agent is about to be told to do is still the
> right thing to tell it.

And, replacing the bare "name all three" bar with a criterion-relative one (the
existing default-to-no-change stance is kept verbatim; only the *test* changes):

> Your default is no change. Act when a landed result contradicts something a
> specific unstarted instruction is relying on, or when a condition of the
> criterion this job is judged against can no longer be met by the plan as it
> stands. Name the result, name the instruction, and name what in it is now
> false. If you cannot name all three, there is nothing to do here, and saying so
> is the right answer.

**Cache shape (L7):** four messages instead of three —
(1) system prompt (static), (2) `graph.context()` — settled points, open
questions, evidence standard: **stable for the job's whole life**, (3) the plan
outline `stateBlock()` — changes only when the plan changes, (4) the frontier
briefs and the event — the delta, last. Today the context and the outline are
concatenated into one message (`revise.go:138-140`); splitting them is what makes
(2) cacheable across every revision pass of a job.

### How it is tested

- `cmd/aforge/revise_order_test.go` — **the guarantee, pinned**: a leaf whose
  consumer is pending must not have that consumer claimed while its `reviseAfter`
  is in flight. Implemented by making the sentinel's client block on a channel
  and asserting `store.Ready` excludes the consumer throughout. This test is the
  deliverable of the §0 correction.
- `internal/plan/revise_test.go` — the frontier render is bounded; a frontier of
  zero nodes produces today's message set byte for byte.
- `internal/resident/revise_test.go` — an `add` past the ceiling is refused and
  noted (W2's fix, re-asserted here because this is the wave that starts firing it).
- Pod: **V5** (the mid-run-adaptation shape, which is what this exists for).
  Metric: at least one `plan_graph` event whose operations are non-empty and whose
  trigger is a landing rather than an overrun. Today: zero in eight runs.

### Three axes

- **Wall time: slightly up per landing, down per job.** The pass already runs on
  the landing path; this makes its prompt bigger, not more frequent — the
  round-trip is unlocked while thinking (`unlockedWhileThinking`, `chat.go:2660`ff),
  so the added latency is the model's, not the lock's. Against that: a rewired
  unstarted node is a leaf that does not have to be redone.
- **Token cost: up per pass, materially.** Briefs are 90-160 words each
  (`brief.go:74-75`); a frontier of five costs ~600-1,000 extra prompt tokens per
  landing. This is the most expensive prompt change in the design, and it is why
  the frontier is *the frontier* — the nodes about to start — and not every
  unstarted node. Offset by splitting the stable context into its own message so
  (2) becomes cacheable.
- **Quality: up, and it is the negative finding of §10 turned positive.** "No
  sibling-result-driven structural change exists in practice" is the sentence
  this wave exists to falsify.

### Risk / rollback

Risk: a sentinel that can now see instructions starts editing them. The doctrine
against that is already the prompt's spine ("the danger here is not that it edits
badly, it is that it edits at all") and stays verbatim; `checkingRule` stays; the
`apply` refusals stay recorded; and W2's `growJob` now bounds `add` for the first
time. Order matters: **do not land W7 before W2.**

Rollback: pass an empty frontier — one line at `reviseOn`, restoring today's
three-message prompt exactly.

**chat.go anchor: `chat.go:2594-2631`. This is the deepest `chat.go` edit in the
design and should be scheduled into a window where the other session is not in
that function.**

---

## W8 — Coverage audit, and a research-shaped task when grounding is missing

### Goal

Two things, one mechanism.

**(a) Coverage.** `internal/plan/audit.go` asks *"can each node be finished with
what it has?"* — a per-node completability question. Nothing asks the dual:
*"is there a required deliverable no node produces?"* V6 built 24 nodes covering
all nine capability areas and still shipped an **import cycle** — a required
property of the whole ("the packages build together") that no node owned. Model
the new pass on `audit.go`'s structure exactly: same concurrency shape, same
`auditWith`-style shared-block reuse, same "answer ok unless something concrete
is missing" register.

**(b) Grounding.** When the coverage pass reports a gap it cannot describe
because nobody knows enough yet, the planner may add a **research-shaped task
from its existing vocabulary** — an ordinary work node whose deliverable is
knowledge. **No new node kind. No new subharness** (explicitly deferred).

### Files and functions

- **New — `internal/plan/coverage.go`** — `Coverage(ctx, client, graph, criterion, shared)`,
  structured on `audit.go:76-140` (`auditWith`): shared catalog block reused when
  the node count has not moved, per-stage goroutines, faults recorded as failed
  stages.
- `internal/plan/plan.go:466-476` — the coverage pass runs **after** `auditWith`
  and **before** `anchorLateStarts`, using the same `auditShared` block. Adding a
  node here is free of a new render.
- `internal/resident/grow.go` — coverage is also a **growth mechanism**: run it
  again when a frontier lands, through `growJob` with `Reason: "coverage"`, so it
  is capped, counted and gated by the criterion like everything else. The
  satisfaction gate (W2) and the coverage pass ask adjacent questions, so at
  execution time **they are one call** — `Satisfied` returns `uncovered`, and an
  uncovered condition with a concrete `missing` is exactly a coverage add.
  That is why W2's schema has an `uncovered[].missing` field.
- `internal/plan/plan.go:118-120` `proportionRule` — cited in the coverage prompt
  so a coverage add cannot become a second planner.

### What the prompts say

`coveragePrompt` (general; the deliverable vocabulary is the criterion's own,
never the orchestrator's):

> You check whether the plan produces everything the finished job is required to
> produce.
>
> You are given the criterion the job is judged against and every node in the
> plan with what it delivers. For each condition of the criterion, find the node
> whose deliverable satisfies it. A condition satisfied by no node is a gap.
>
> Look hardest at the conditions that are about the whole rather than the parts —
> a property that only holds once every piece is together is exactly the kind
> nobody's own piece produces, and exactly the kind that goes missing.
>
> Name each gap as the condition it belongs to and the deliverable that is
> absent. Do not name a node that would check another node's work: checking is
> not a deliverable, and a plan whose gaps are filled with inspection grows
> forever. Do not name a gap that a node merely covers imperfectly. Missing is
> the test, not thin.
>
> When every condition has an owner, say so. That is the common answer and it is
> the right one.

Schema: `{"gaps": [{"condition": string, "missing": string, "after": [int]}]}`
— `after` names the existing nodes whose results the new work would consume, so
a coverage add arrives with its edges rather than needing `anchorLateStarts`.

`groundingPrompt` — a second, small question asked only when a gap's `missing`
is itself unknown:

> Some gaps cannot be described because nobody yet knows enough about the subject
> to describe them. Say whether that is the case here.
>
> If it is, the work that closes it is work whose whole deliverable is the
> knowledge itself: what was found, well enough that the piece that consumes it
> can be written without going and looking again. That is an ordinary piece of
> work, planned and handed over like any other, and everything that reads it
> receives its result the way it receives any other result.
>
> Say no when the subject is already settled well enough to state what is
> missing. Work that goes looking for something already in hand is the most
> expensive kind of nothing.

**Cache shape (L7):** the coverage pass reuses `auditShared` — the same ~8 KB of
goal, premise and node list that `bind`, `size` and `audit` already share
(`plan.go:434-440` explains the sharing). At build time this is genuinely free.
At execution time it is folded into W2's `Satisfied` call, so the append-only
landed table is the prefix and the trigger is the tail.

### How it is tested

- `internal/plan/coverage_test.go` — a criterion condition with no owning node
  yields exactly one gap; a condition covered imperfectly yields none; a proposed
  "check node X" gap is refused; `after` edges are applied through `AddNeed` so
  no cycle can be closed.
- `internal/plan/coverage_test.go` — the grounding question is not asked when
  every gap has a concrete `missing`.
- `internal/resident/grow_test.go` — an execution-time coverage add is subject to
  the ceiling, the round counter and the criterion gate.
- Pod: **V6**. Metrics: (1) at delivery, zero uncovered criterion conditions;
  (2) the V6 defect class — a whole-graph property that no node owned — has an
  owner; (3) node count does not sprawl: the ceiling holds and `job_growth`
  refusals are visible.

### Three axes

- **Wall time: neutral at build (one concurrent pass beside `audit`, sharing its
  block), up slightly at execution (a coverage add is a node that must run).**
  Against that, V6's import cycle meant 13 nodes never ran and the deliverable did
  not build — the wall time of a job that produces nothing usable is infinite.
- **Token cost: one extra call per plan, plus zero extra at execution** (folded
  into W2's gate). The build-time call reuses an already-rendered block, so it is
  prompt-cheap.
- **Quality: the wave with the clearest quality mandate.** V6's coverage was
  broad and its integration was absent. A criterion-driven ownership check is what
  distinguishes a plan that covers the subject from a plan that produces the
  thing.

### Risk / rollback

Risk: coverage becomes a second planner and the graph grows every time it is
asked. Bounded by: the "checking is not a deliverable" clause (the same doctrine
as `checkingRule`), the criterion being closed and small, `proportionRule`, and
W2's ceiling and round counter. **Do not land W8 before W2.**

Rollback: `plan.Options.Coverage bool` at build; `Reason: "coverage"` disabled in
`growJob` at execution. Two independent switches.

**chat.go anchors: none.**

---

## W9 — Minimal edges: transitive reduction and specific splice edges

### Goal

Two structural over-connections, both of which cost concurrency:

1. **`anchorLateStarts` (`internal/plan/graph.go:699-766`, called at
   `plan.go:472`)** wires any stage > 1 node with no inputs to *every* earlier
   unconsumed node. It is a structural backstop for bind's deliberate
   under-connection, and it is also **stage used as a dispatch gate** — the exact
   thing L8 forbids. Replace it with **transitive reduction after bind + audit +
   coverage**: keep the same protection (a late node with nothing to wait for is
   wrong) but derive the edges from the real dependency closure, then delete every
   edge implied by another path.
2. **`Graph.Splice` (`internal/plan/graph.go:575-655`)** gives *every* root child
   of a subtree the parent's whole `Needs` list, and the parent gathers every
   sink. Wholesale fan-in preserved where specific child-to-consumer edges are
   derivable. With W5, splices happen mid-run and this multiplies.

§10's V2 is the cost: an eight-node DAG that was *effectively a chain* —
in-degree `{1:5, 2:1, 3:1}`, stages 1→5 — for work whose parts were independent.

### Files and functions

- **New — `internal/plan/reduce.go`** — `(g *Graph) reduce() int`, standard
  transitive reduction over `Needs`: an edge `u → v` is removed when another path
  `u ⇝ v` exists. `g.reaches` (`graph.go:329-361`) already provides the walk, and
  `hasCycle` (`:395`) guarantees the DAG property the reduction assumes.
- `internal/plan/graph.go:699-766` `anchorLateStarts` — replaced by
  `anchorUnreachable()`: a node with no inputs is wired **only** to the sinks it
  is not already transitively downstream of, and stage is used to *order* the
  candidates, never to *create* an edge. Keep the deterministic sort and the
  `AddNeed`-only rule (nothing here may close a cycle).
- `internal/plan/plan.go:466-476` — call order becomes
  `audit → coverage (W8) → anchorUnreachable → reduce`.
- `internal/plan/graph.go:614-624` `Splice` — a root child inherits only the
  parent needs it can actually consume; when the sub-graph's own internal edges
  already reach a parent need, the inherited edge is dropped. Then `reduce()` over
  the spliced region. The existing fallback ("no sink survived the remap; fall
  back to every child") stays as the safety net.
- `internal/resident/planadapter.go:105-130` `resolveNeed` — unchanged. Its
  container-flattening is correct and its comment records why (a "connect the
  scans" node that ran first against an empty workspace).

### What the prompts say

**Nothing.** This is a deterministic pass, and that is deliberate: §7's F6 makes
the case that *"a deterministic post-pass … would be more reliable than another
prompt paragraph"* about rules `fanout.go:72-78` and `bind.go:74-77` already
state and models already disobey. No new call, no new tokens.

### How it is tested

- `internal/plan/reduce_test.go` — reduction preserves reachability exactly
  (property test over generated DAGs: for every pair, `reaches` before ==
  `reaches` after); never removes an edge on a unique path; is a no-op on a graph
  that is already reduced.
- `internal/plan/graph_test.go` — `Waves()` (`:780`) width is monotonically
  non-decreasing under reduction, on every existing fixture. **This is the
  concurrency claim, asserted directly.**
- `internal/plan/graph_test.go` — no stage > 1 node is left with zero inputs
  (`anchorLateStarts`'s original guarantee, preserved).
- `internal/plan/expand_test.go` — a spliced subtree's children do not each carry
  the parent's whole fan-in when internal edges already reach it.
- Pod: **V6** and **V4**. Metrics: edge count (V6: 69 `feeds_into` edges over 24
  nodes) down; in-degree distribution flattened; `Waves()` width up; ratio and
  peak concurrency up — **the last two only readable after W0**.

### Three axes

- **Wall time: down, conditional on W0.** V2's chain shape is the direct cost;
  every removed implied edge is a node that can start earlier.
- **Token cost: down, second-order.** `Needs` is also the context routing table
  (`anchorLateStarts`'s own comment says so) — every spurious edge is a
  dependency digest pasted into a leaf that did not need it, at
  `store.MaxDigestBytes` each. With prompt-to-completion ratios of 29:1, this is a
  real line item.
- **Quality: neutral to slightly up.** Fewer irrelevant digests is less context
  pollution — the exact hazard `expandScope`'s comment calls *"context pollution
  with extra steps"*.

### Risk / rollback

Risk: reduction removes an edge that was carrying *context* rather than
*ordering*. This is real: `Needs` is dual-purpose. Mitigation: reduce only edges
implied by a path **within the same job**, and never reduce an edge into a
gathering node (`Kind == KindSynthesis` or a node with children) — a gatherer's
fan-in *is* its context and the whole point of it. Encode that as a predicate in
`reduce()`, not as a caller's responsibility.

Rollback: `plan.Options.Reduce bool`, and `anchorUnreachable` falls back to
`anchorLateStarts` verbatim (keep the function, do not delete it, for one
release).

**chat.go anchors: none.**

---

## 2. Acceptance battery

Reuse the §10 pod shapes verbatim, same model (`deepseek/deepseek-v4-flash` via
OpenRouter), same runner (`/root/v10/runv.sh`), same analyzer (`/root/v10/an.py`),
same co-residency discipline: **launch each A/B pair into the same loaded
window**, since that is the only way the wall-time comparisons in §10 were fair.

The six shapes, as §10 defines them:

| run | question it answers |
|---|---|
| **V1-swe** | does a single coding leaf converge? |
| **V2-parswe** | parallel coding leaves + git race in one workspace |
| **V3-mixed** | per-node harness diversity |
| **V4-briefs** | fan-out integrity on prose; the equal-output cost comparison |
| **V5-adapt** | mid-run adaptation |
| **V6-ldg** | plan depth, shape and coverage at 24 nodes |

### Which run proves which wave moved which axis

| wave | runs | axis proved | the exact metric |
|---|---|---|---|
| **W1** spec durability | V1, V5 | **quality** | Replacement/extension node's `Spec.Done` is byte-identical to the node it replaces. `SELECT COUNT(*) FROM events WHERE kind='plan_graph'` ≥ 1 for task-scale jobs (today: 0). |
| **W2** growth governor | V5, V6 | **wall, cost** | No job's node count exceeds `maxJobNodes`. `job_growth` rows show rounds and refusals per reason. V4's serial overrun tail (171 s, ratio 0.77 in V5) shortens or disappears. At least one refusal cites coverage, not a cap. |
| **W3** harness templates | V1, V2 | **quality**, then wall | `go build ./... && go test ./...` on the produced workspace. V1's "compiles, tests FAIL, never converged" must become converged-or-honestly-failed. The rendered `swe` goal contains the method and the criterion. |
| **W4** claim-time selection | V3, V6 | **wall, cost** | `COUNT(*) FROM events WHERE kind='node_worker_changed'` → toward 0 (baseline: 8 across the battery). Seconds burned before each escalation (V6: 221 s, 522 s) → 0. V3's generalist-shipped-working-code case must not be re-routed to a specialist. |
| **W5** JIT expansion | V6, V2 | **wall** (needs W0), **quality** | Parent-depth histogram max ≥ 3 (baseline: 2, in every run of the battery). Ratio (leaf-seconds ÷ leaf-span) up from V6's 2.27. Zero nodes decomposed only after exhausting a budget. |
| **W6** joint objective | V4, V6 | **cost** | V4 is the honest test: identical output on both arms. Baseline fixed arm was $0.0978 / 382 s vs pre-fix $0.0627 / 246 s. Spend at equal output must fall. Also `cached_tokens > 0` — which after W0-W9 should no longer be 0 in every run. |
| **W7** sentinel lands | V5 | **quality** | ≥ 1 `plan_graph` event with a non-empty operation list whose trigger is a landing, not an overrun (baseline: 0 in eight runs). Plus the unit test pinning the dispatch-ordering guarantee. |
| **W8** coverage + grounding | V6 | **quality** | Zero uncovered criterion conditions at delivery. The V6 defect class — a whole-graph property no node owned (the import cycle) — has an owner. Node count does not sprawl. |
| **W9** minimal edges | V6, V4 | **wall** (needs W0), **cost** | Edge count down from V6's 69 over 24 nodes; in-degree distribution flattened from `{1:3, 2:8, 3:9, 6:1, 8:1, 9:1}`; `Waves()` width up; peak concurrency up; total dependency-digest bytes delivered to leaves down. |

### Cross-cutting metrics to record on every run

Beyond the per-wave metric, capture these on all six shapes every time, because
the joint objective is only readable as a triple:

- **wall** (`.wall`), **rc**, **spend**, **prompt/completion tokens**, and
  **`cached_tokens`** — the last is the single largest untouched token lever and
  was 0 in every run of the battery.
- **peak concurrency** and **ratio** from overlapping `started_at`/`finished_at`.
- **parent-depth histogram** and **in-degree distribution**.
- **`subharness` vs `splice_subharness`** per node (the F1b diversity evidence).
- **delivered artifacts actually build/read**, judged the way §10 judged them —
  by running the build and the tests, not by reading the narration. §10's defect
  #3 (outcome narration contradicting the artifacts on disk) means the run's own
  summary is not admissible evidence.

### Known open defects this blueprint does not close

Carried forward from §10's "new defects found post-fix", so they are not
mistaken for regressions caused by these waves:

1. swe delivery-node crash — *"No user message found in stream"* on a job whose
   deliverable builds and passes.
2. swe internal scheduler stall — *"resource pause persisted for 12 cycles"*
   (V6: 513 s and 210 s burned).
3. Outcome narration contradicting the artifacts.
4. swe spend under-reported (~35× discrepancy in $/token between V1 and V2).
5. swe gives every session its own module cache under `/tmp/codeaf-scratch/ses_*/go-mod`,
   which filled the pod's root filesystem mid-battery.

(4) in particular corrupts the measured line W4 and W6 depend on — a worker whose
cost is journalled as $0.0000 cannot be preferred or de-preferred on evidence.
**It is worth fixing before W6's measured-line instruction is trusted**, even
though it is out of scope here.

---

## 3. Build discipline

- End every change with `make check`.
- The only binary is `bin/aforge`; delete any stray `./aforge` at the repo root.
- Scope: no wave in this blueprint touches `internal/tui2` or the resident
  narrator / noise-law files (`narrator.go`, `narrator_test.go`,
  `threadnoise_test.go`), which another session owns.
- `cmd/aforge/chat.go` is edited by five waves at five named anchors (A1-A5 plus
  `reviseOn`); every one of them is a call substitution into a new file in
  `package main`, so the contention window is minutes, not hours.
