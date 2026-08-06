# Learning that compounds — where aforge stands, what the field knows, what to build

This document does three things: states honestly where aforge's learning
machinery is today (verified against the code, file:line), condenses what the
2023–2026 academic literature has actually established about agents that
improve from experience, and derives the architecture moves — tailored to what
already exists here, and to one governing principle:

> **Capabilities must be emergent, never authored.** When the agent needs a
> script, a tool, a workflow, a habit — the architecture supplies the *loop*
> that grows it, verifies it, and retires it. We never hard-code the content.
> Anything else is a program wearing an agent costume.

---

## Part 1 — Where aforge stands: excellent write path, open loops

The honest audit: aforge has unusually thoughtful *instrumentation* — the
verdict taxonomy separating control flow from evidence (`provider/verdict.go`),
the Rasch ledger with every brake justified by a measured collapse
(`router/ledger.go:61-86`), the recalibration guard that refuses to learn when
≥90% of tasks overran because "that points at the budget, not the ruler"
(`profile/profile.go:195-221`), evidence-linked belief audit in consolidation
(`chat.go:1343-1380`). Almost every constant cites the experiment that set it.

But most loops do not close. The distance to a system that visibly compounds
is not architectural — it is wiring. The six open circuits, each with its
consuming machinery already built:

1. **The ruler is recalibrated and never installed.** `plan.UseAnchors` is
   called at exactly one line (`cmd/aforge/main.go:117`, the `plan`
   subcommand). Both `run` and `chat` compute and save new anchors and then
   size every future plan against the built-in prior.
2. **The retrospective latches shut.** `settledJobSketches` caps at 12
   (`retrospect.go:94`); once 12 jobs have settled, `len(jobs) <
   lastReflectedJobs+1` is permanently true (`retrospect.go:62`) and the one
   loop built to learn from the series runs exactly once per process, ever.
   (The counters are also in-memory only — a restart re-reflects over the
   same 12 jobs.)
3. **The resident surface never sees the router.** `ClientFor`
   (`config.go:212`) bypasses the panel, so the ledger, shape-keyed ratings,
   exploration, and leaf escalation — the most sophisticated learning
   machinery in the repo — run only headless. Chat verdicts are computed and
   discarded (`Report` finds no observer).
4. **Consolidation severs the evidence link it exists to use.** Consolidated
   lines are recorded with empty `nodeID` (`notebook.go:153`); one pass
   through consolidation and a belief is permanently unauditable.
5. **The delivery gate's verdict teaches nothing.** A failed gate — the
   highest-quality failure signal in the chat path — goes to a system message
   (`chat.go:263-267`) and nowhere else: not the profile, not the notebook,
   not a Verdict.
6. **No recall at plan time.** `plan.Ground` takes `(ctx, client, goal)` and
   nothing else. No planning pass has ever read a notebook line or a prior
   fold; folds have no FTS index; the executor has no pull tool; the CAS is
   dead code. This is the architecture's own Decision 6 / M4, unbuilt.

Smaller but consequential: successful leaves inside a project never distill
(`resident.go:484-486` — failures teach at leaf granularity, successes only at
job granularity); "experiment over faith" is three paragraphs of prompt with
no data structure, detector, or counter — nothing can tell whether it has ever
fired; the profile is keyed by the talking model, not the model that did the
work (`chat.go:1062` vs the TUI model picker); gate/revision-sentinel signals
are captured and dropped.

**Rule derived from this audit: no new learning mechanism until its loop
closes.** Every open circuit above was a beautiful write path waiting for a
reader. The wiring fixes come first because they are also how we *validate*
the loops before adding more.

---

## Part 2 — What the literature actually established (2023–2026)

Condensed from a sweep of Stanford / CMU / MIT / Berkeley / Princeton /
Tsinghua / UBC work. Five load-bearing, replicated results:

1. **Verbal reflection, stored and replayed in context, is the learning
   signal.** Reflexion → ExpeL → AutoGuide → ACE (Stanford 2025). GEPA
   (ICLR 2026) is the sharpest number: reflective prompt evolution beats
   GRPO-style RL by +6pp average with **up to 35× fewer rollouts** — language
   extracts more bits per episode than a scalar reward. No weight updates
   required anywhere in this lineage.
2. **What transfers across dissimilar tasks is procedural artifacts, not
   declarative text.** Voyager's executable skill library, CMU's Agent
   Workflow Memory (+51% relative on WebArena, gap *widening* with
   distribution shift), TroVE's induced toolboxes (same accuracy, 79–98%
   smaller), Absolute Zero's code-self-play transferring +15pp to math.
   Optimized prompts are the least transferable learned artifact; verified
   code is the most.
3. **The evaluator, not the proposer, is the load-bearing component.** Every
   large self-improvement gain (Darwin Gödel Machine 20→50% SWE-bench, SICA
   17→53%, Voyager, AlphaEvolve) validates proposals against an *external,
   executable* check. Every loop using the model's own judgment as reward
   stalls, degrades (intrinsic self-correction made GPT-4 *worse*, −6.5pp,
   ICLR 2024), or gets hacked (DGM's agent deleted the hallucination
   detector's logging markers to score perfectly — and hacked more when the
   checker was visible to it).
4. **Incremental delta updates, never monolithic rewrites.** ACE names the
   failure modes: *brevity bias* and *context collapse* — letting a model
   re-summarize the whole memory each cycle destroys it. Winners maintain
   itemized entries with add/update/supersede operations. (aforge's
   scope-bucketed consolidation with `replaces` is already this shape.)
5. **Retrieval scoring beyond similarity wins.** Recency + importance +
   relevance (Generative Agents), graph-associative retrieval (HippoRAG,
   +20% multi-hop at 10–20× lower cost), time-aware indexing (LongMemEval).
   And a 2026 ablation warning: extracted-fact memories sometimes lose to
   *verbatim chunk* retrieval — keep pointers to the raw material, not only
   digests.

Five measured warnings:

- **Self-preference bias is causal** (NeurIPS 2024): models score their own
  outputs higher, and self-recognition training increases it. A judge inside
  the loop it judges is a biased reward.
- **Self-improvement inflates confidence**: iterated self-improvement raises
  ECE monotonically (confidence rises, accuracy doesn't). Recalibrate at
  every loop iteration, not post hoc.
- **Prompt/memory-space forgetting is real**: self-evolving agents show
  capability regression as new memories crowd out old ones
  (LifelongAgentBench line); nobody has a principled consolidation theory —
  aforge's evidence-linked audit is *ahead* of published art here.
- **Self-proposed curricula drift without external anchors** (Absolute Zero's
  "uh-oh moment"); generated practice tasks must carry their own verification
  (Self-Challenging's Code-as-Task: instruction + verifier + solution +
  failure cases).
- **No verifier, no loop**: nearly all headline results depend on cheap
  ground truth. Learning from a single-shot, unlabeled, heterogeneous real
  job stream is the field's named open problem.

Where aforge is already past the frontier — worth knowing, because it means
there is no paper to copy: a **live capability/cost model of one's own
execution** (the profile + ledger) and **an agent running controlled
experiments on its own strategies over a real task stream** are both
explicitly identified as open problems with no top-venue treatment.
Evidence-linked belief revision is a 2026 arXiv frontier with no replicated
winner. These three are aforge's chances to define the art, not follow it.

---

## Part 3 — The architecture: loops that grow capabilities

Ordered. Each proposal states the mechanism, the emergent content, and the
external check — because a loop without all three is either a hard-coded
feature or a hallucination amplifier.

### 3.0 Close the six circuits (prerequisite, ~wiring)

Install anchors in `run` and `chat` (guard the package-global `Anchors` once
two surfaces write it); fix the retrospective latch (count settled jobs from
the store, not the capped sketch slice, and persist the watermark as an
event); route chat through the router (or at minimum attach the ledger
observer so chat verdicts land); thread the surviving original's `NodeID`
through consolidation; feed gate verdicts into profile + notebook; record with
the model that actually worked. None of this is new design — it is the
existing design, finished.

### 3.1 The Skill Forge — one mechanism, emergent tools

This is the emergent-capability principle made concrete, and it is the
literature's strongest line (Voyager/AWM/TroVE) fused with machinery aforge
already has.

**A skill is a fold that executes.** Concretely: a directory in the CAS —
`run.sh` (or any executable), a one-line doc, a `check` (a fast self-test),
and provenance (the jobs that taught it). Indexed on the same shelf as facts:
a `skill` kind in the notebook, scope-keyed (`tool:ffmpeg`, `repo:…`,
`domain:…`), BM25-retrievable, `uses`/`last_used` maintained. The CAS stops
being dead code; this is what it was for.

**Where skills come from — never from us:**
- *Distiller candidacy.* The distiller already reads every job's transcript.
  One added judgment: "did this job build a procedure a future job would
  reuse — a script written in the workspace, a command sequence repeated, a
  detour that worked?" If yes, it emits a skill *candidate* (the artifact
  already exists in the workspace; the distiller names it and states what it
  is for). Candidates are beliefs, not capabilities — status `candidate`.
- *Retrospective promotion.* The two-independent-occurrences bar the
  retrospective already holds becomes the promotion trigger: a candidate (or
  a repeated procedure across jobs) seen twice → promote to trial.
- *Trial before trust.* Promotion splices a cheap `origin: self` trial node:
  run the skill's `check` in a clean workspace. Green → status `active` and
  the skill enters retrieval. Red → the candidate is superseded with the
  failure as evidence. **The evaluator is execution, never self-judgment.**

**How skills are used — generically:** active skills matching a leaf's scope
cues are surfaced two ways, both content-free: `~/.aforge/skills/bin` goes on
the `sh` tool's PATH, and the contract writer sees the matching skills' doc
lines ("this shelf exists; use it if it fits"). No skill is ever named in
prompt doctrine; retrieval decides.

**How skills die:** the machinery already built for beliefs — `uses` decay
(TroVE trims by frequency), supersession on a distilled failure ("skill X
broke on Y"), and the consolidator's staleness-by-what-it-claims judgment
applied to `skill` lines like any other. A skill that stops earning
retrieval stops being offered; one that fails in the wild is retired with
its evidence on record.

The hard-coded surface area is five things: the candidate judgment in one
prompt, a `skill` kind, a trial splice, a PATH entry, a retrieval call.
Everything the shelf ever contains is emergent.

### 3.2 Experiments become mechanical

"Experiment over faith" currently requires the model to spontaneously
cooperate with itself three times across three prompts. Give it a spine:

- A fact kind `unsettled` (the consolidator already *writes* competing pairs;
  make the pair machine-readable: two approaches, their scopes, the evidence
  seqs behind each).
- The compiler's trial-shaping rule fires on a *retrieved `unsettled` fact*,
  not on prose intuition — and marks the spliced subtree as a trial for that
  fact (a field on provenance, not a new node kind).
- When a trial subtree lands, the distiller is *told* it was a trial and
  which fact it tests; its verdict supersedes the `unsettled` pair with the
  winner. A trial that produced no verdict is itself recorded — "ran, didn't
  settle" is evidence too.
- One counter in the store answers "has this ever fired?" — the question the
  current design cannot answer about itself.

This is the router's exploration doctrine ("an experiment that cannot be
re-run is an anecdote") lifted from model-choice to approach-choice. The
literature has no formal treatment of an agent A/B-testing its own strategies
on a live stream; this plus the ledger would be the first coherent one.

### 3.3 Recall that reaches the whole life (M4, sharpened)

Decision 6 as written, plus three upgrades the literature earned:

- FTS5 over fold digests, node summaries, and **verbatim intents** (the
  intent was preserved unedited precisely to be the recall key — index it).
- Retrieval scored recency + uses + relevance, not similarity alone; the
  facts table already carries all three signals.
- **Digest for recall, pointer for depth**: recall injects the bounded
  digest, but every digest carries its CAS/workspace pointers, and the
  executor gains the pull tool (`recall` — query folds/notebook, read
  pointed-at material). Extracted memories lose to verbatim sources often
  enough that the pointer must always ride along.

Ground consumes recall ("you have worked here before"); the contract writer
consumes skills; the gate consumes standing preferences (it currently judges
"would they accept it" without knowing what they always want). Same shelf,
three readers.

### 3.4 The practice loop — active learning, after M3 rails

The self-curriculum recipe is now well-replicated (WebRL, Absolute Zero,
Self-Challenging): generate tasks at the frontier of ability, from your own
failures, each carrying its own verifier. aforge's version, gated on M3's
rails existing (a self-firing loop without rails is the hazard the
architecture doc already refuses):

- The profile knows the frontier: leaf shapes and scopes with failure share
  in the learnable band (roughly 25–75% — WebRL's band; certain failure
  teaches nothing, certain success teaches less).
- Idle maintenance (where consolidation already lives) may splice
  `origin: self` practice jobs *only* in domains where verification is
  execution (code, data transforms, anything with a check it can write
  first — Self-Challenging's Code-as-Task shape: task + verifier + known
  failure case).
- Practice results feed the profile, the ledger, and the skill forge — not
  the user's thread. Budgeted from the global daily rail, lowest priority,
  preempted by any real work.

This turns dead time into calibration and skills. The system gets better at
what it measurably struggles with, without anyone authoring a curriculum.

### 3.5 Doctrine that learns: per-scope playbooks for contracts

The contract pass is prompt doctrine we wrote once. ACE's result says the
context itself should accrete: give the contract writer a per-scope
**playbook** — itemized strategy bullets ("in this repo, tests run via X";
"PDF extraction: route A beats route B — settled by trial #…") maintained by
the distiller/consolidator as *delta updates* with the same
evidence/supersession machinery as facts. Never rewritten wholesale (that is
the measured context-collapse failure), bounded per scope, aged like
everything else. The contract writer composes: our fixed doctrine + the
scope's earned playbook. Over time the second part dominates — doctrine
becomes something the system wrote for itself.

### 3.6 The graph is the curriculum: subgraph templates as procedural memory

The permanent task graph is not just a scheduler — it is the richest learning
substrate in the system, and mostly unexploited. Three reads, in order of
ambition:

- **Every run is replayable experience.** The event journal already records
  every decomposition, claim, verdict, and fold with provenance. The profile
  and ledger read slices of it; nothing reads *shapes*. Record per-scope the
  structural features of settled jobs — fan-out width, depth, which edge
  patterns, where overruns and revisions clustered — and the ruler/planner
  gains structural priors, not just size anchors.
- **Fold successful subtrees into parameterized graph templates.** This is
  CMU's Agent Workflow Memory lifted from action-level to plan-level: when
  the retrospective sees the same *shape* of job succeed twice (same stage
  structure, different parameters), the fold becomes a template — stages,
  edges, contracts, with the varying parts as named slots. Recall at spine
  time: "this goal matches a proven shape; instantiate it" — the planner
  spends its calls on what is genuinely new instead of re-deriving a known
  decomposition. Templates are skills for the planner: execution-verified
  (they ran), evidence-linked (the jobs that taught them), retired by the
  same aging machinery. `aforge adopt` (shareable routines, PRODUCT.md)
  falls out of this for free — a template is exactly the shareable unit.
- **Standing goals become learned, not declared.** Once M3 lands, a
  recurring template + a recurring trigger pattern in the retrospective is a
  candidate standing goal, proposed through the decision inbox — the agent
  noticing its own job.

### 3.7 Data layer and transport: settled questions

Asked directly: do we need a different database, or websockets for
inter-task communication? **No, and no — with reasons worth recording.**

- **SQLite + FTS5 stays.** The workload (one writer, many readers, CAS
  claims, subtree queries, BM25 recall) is exactly the WAL profile, and the
  event journal already gives replay, audit, and migration freedom. A vector
  DB is refused until a *measured* recall failure demands embeddings
  (Decision 6's own doctrine — and the literature's lexical-vs-vector
  results back it). A graph DB solves a scale of traversal we are years
  from; the recursive CTEs are fine. What the data layer actually needs is
  what Part 3 adds: FTS over folds/intents, a `skill` kind, trial marks on
  provenance — schema evolution, not a new engine.
- **No websockets, no message bus, no inter-task channels.** Decision "not
  a message bus" holds: nodes never talk to each other — they read folds
  and the store (pull), and STATE edges order mutation. Everything a push
  channel would carry is already an event in the journal; a reader that
  wants "push" tails the journal (the TUI already does). When remote attach
  or webhook wake arrives (M3), it is one unix socket / one loopback HTTP
  listener on the daemon — transport for *lenses*, never for tasks. The
  moment tasks message each other directly, provenance breaks and the
  journal stops being the truth; that trade buys nothing the pull model
  lacks.
- **The two-surface covenant, restated as law.** Headless (`aforge plan|run`)
  is the benchmarked, atomic, linear-harness path and stays byte-for-byte
  first-class: learning features specialize it **by addition only** (a
  recall input, an extra tool, a better ruler) — the generic loop is the
  completion floor, and mis-specialization must degrade to baseline, never
  to failure. The resident surface (`aforge chat`) is where the personal,
  continually-learning system lives: notebook, folds, skills, templates,
  retrospective, practice. One store, one set of loops — two doors.

### 3.8 Calibration discipline (cross-cutting)

Three rules from the failure-mode literature, cheap to honor now:

- **Judges stay outside the loop they judge.** The delivery gate and trial
  verdicts should run on a different model than the worker when the panel
  allows (self-preference is causal, measured). The router makes this free.
- **Checkers stay invisible.** Anything that grades the agent (gate prompt,
  trial checks, practice verifiers at grading time) is never in the graded
  context. DGM's metric-deletion hack was enabled by a visible checker.
- **Recalibrate inside every loop.** Confidence inflates under
  self-improvement; the profile/ledger updates land at the same cadence as
  the learning they measure, and `NeedsRecalibration`-style guards (refuse to
  learn from confounded evidence) are the template for every new loop.

---

## Sequencing

| step | what | why first |
|---|---|---|
| 1 | Close the six circuits (3.0) | Existing learning starts compounding; validates every loop before new ones are added |
| 2 | Skill forge (3.1) + mechanical experiments (3.2) | The emergent-capability core; both mostly reuse distiller/retrospective/consolidator machinery |
| 3 | Recall M4 (3.3) | Planner and gate finally read what the system knows; skills/playbooks ride the same shelf |
| 4 | Playbooks (3.5) + calibration discipline (3.8) | Doctrine accretes; loops stay honest |
| 5 | Practice loop (3.4) | Only after M3 rails; the first three steps supply its frontier map and its verifiers |

The test for every future proposal stays the user's principle: *what can the
agent now learn to do on its own that we didn't write?* If the answer is
"nothing — we just added a feature," it doesn't belong in this document.

---

## Sources (primary)

ACE arXiv:2510.04618 · Dynamic Cheatsheet arXiv:2504.07952 · AWM
arXiv:2409.07429 · TroVE arXiv:2401.12869 · Voyager arXiv:2305.16291 ·
Reflexion (NeurIPS 2023) · ExpeL (AAAI 2024) · Generative Agents (UIST 2023)
· AutoGuide (NeurIPS 2024) · AutoManual (NeurIPS 2024) · Self-Generated
In-Context Examples arXiv:2505.00234 · MemGPT arXiv:2310.08560 · HippoRAG
(NeurIPS 2024) · A-Mem arXiv:2502.12110 · LongMemEval (ICLR 2025) · ADAS
arXiv:2408.08435 · Darwin Gödel Machine arXiv:2505.22954 · SICA
arXiv:2504.15228 · MIPROv2 arXiv:2406.11695 · TextGrad (Nature 2025) · Trace
arXiv:2406.16218 · GEPA arXiv:2507.19457 · WebRL arXiv:2411.02337 · Absolute
Zero arXiv:2505.03335 · Self-Challenging Agents arXiv:2506.01716 · OMNI-EPIC
arXiv:2405.15568 · RouteLLM arXiv:2406.18665 · IRT-Router (ACL 2025) ·
KnowSelf arXiv:2504.03553 · Cannot-self-correct arXiv:2310.01798 ·
Self-preference (NeurIPS 2024) · Beyond Accuracy arXiv:2504.02902 ·
LifelongAgentBench arXiv:2505.11942 · Self-evolving agent surveys
arXiv:2507.21046, arXiv:2508.07407
