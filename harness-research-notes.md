# Agentic Harness Research Notes — arXiv, Aug 2026

Mechanisms from ~25 recent papers, read for practical harness-building value.
All papers are unreviewed preprints; benchmark numbers are self-reported.
Focus: transferable mechanisms, not benchmarks-for-benchmarks'-sake.

---

## The meta-finding first

Two camps, not contradictory:

- **Evolve the harness, freeze the model** (HarnessCompass, Harness-R1, One Recipe,
  HSI, Ouroboros, EvoHarness-RL). Sharpest empirical result: harness-evolution gains
  come from **compensating recoverable execution defects** — the gap between what the
  base model *can* do and what it *does* under a bare scaffold. It closes the gap; it
  does not raise the capability ceiling. So harness work is high-leverage exactly up to
  the model's ceiling, and the first job is measuring that gap.
  - One Recipe, Many Harnesses: https://arxiv.org/abs/2608.10178
- **Keep the harness dumb, train the weights** (Microsoft's Faraday): 5 tools, linear
  append-only context, no compaction, "maximally permissive"; capability improved via
  GRPO on a 27B model, explicitly *not* by complexifying the harness. Their 27B agent
  beats Claude Opus at paper replication. If you can post-train, a simple harness +
  good reward beats a clever harness.
  - Training AI Scientists to Replicate Research: https://arxiv.org/abs/2608.13331

---

## 1. State architecture: separate task state from trajectory

The single most transferable pattern, from Alibaba's LongHorizon-Harness
(https://arxiv.org/abs/2608.01964):

- **Manage-Execute-Audit rounds.** Task state lives *outside* the context as structured
  records (requirements / artifacts / facts, each marked pending/completed/blocked/untrusted
  with pointers to supporting evidence). It is updated **only from independent audit
  evidence** — executor self-reports never flip a record to completed.
- **Fresh-context executor per round.** Each round is a bounded contract (goal,
  acceptance criteria, boundary constraints, relevant state records). The executor's raw
  trajectory is *discarded* after the round; only its report survives to audit. No
  context rot, no wrong-self-assessment propagation.
- **Role separation as capability control.** Manager has *no* environment interface
  (decides from state + audits only); executor is the only role allowed to mutate;
  auditor is read-only. You can't talk yourself into believing you finished.

Argus (https://arxiv.org/abs/2608.05144) adds the production details worth stealing:

- Continuity across fresh sessions via one plain `CHECKPOINT.md` (durable state,
  evidence refs, open questions, next step) — not a database.
- **Termination by named thresholds** (max rounds, no-progress count, escalation count,
  backend-failure count), not by reviewer verdict.
- **Two-tier goal contract**: semantic clarifications move freely; the precise objective
  changes only with explicit recorded confirmation. Intent stable, wording negotiable,
  target moves only with authority.
- **Skills exposed, not injected**: publish library paths, let the acting agent
  retrieve; no matcher pre-decides relevance.
- Learning/state changes settle **once per mission boundary**, never per round.

---

## 2. Harness as an evolvable program

**Decompose into typed slots.** One Recipe (https://arxiv.org/abs/2608.10178):
prompt P, workflow hooks W, lessons memory M, tool implementations T.
HarnessCompass (https://arxiv.org/abs/2608.01918) splits the same way: *structural*
(tools, middleware, sub-agents) vs *guidance* (prompt, tool descriptions, skills,
memory) — and **evolves the two tracks separately, then merges** surviving edits,
because joint optimization causes interference.

**The four lifecycle hooks** from Harness-R1 (https://arxiv.org/abs/2608.02276) — the
best concrete edit surface, essentially an API for "harness as program":

1. `episode-init` — set starting context/state
2. `pre-decision` — augment context with retrieved guidance/constraints before the model decides
3. `pre-action` — canonicalize, rewrite, or **veto** the proposed action before it hits the environment
4. `post-feedback` — inspect the observation, trigger recovery when the trajectory stalls

They RL-train a small "engineer" model to emit executable patches over these hooks from
compacted failure packets; reward = rerun the same task batch, take the delta; invalid
patches score zero. Even without RL-training, those four hooks are the right seams for
any automated or human harness editing.

**The generalization gate** (HarnessCompass, https://arxiv.org/abs/2608.01918): every
candidate edit must pass before installation —

- Reject any edit mentioning a specific task instance, test function, or private symbol;
  reject code branches triggered by task-exclusive tokens. What's admitted: a reusable
  decision criterion + an applicability condition evaluable on unseen tasks.
- **Capability edits must be code** (tools/middleware/sub-agents); **guidance edits
  must be prompt/memory** — never encode guidance as executable logic, because guidance
  has no reliable trigger condition.

**Failure attribution as a typed, advisory dossier** (TRIAGE, One Recipe
https://arxiv.org/abs/2608.10178): two evidence channels — verifier-side (scored
submission, environment state, which stages didn't run) and budget-side (termination
reason, resource use — distinguishes "ceiling-interrupted" from "completed but wrong").
Attribution types (verifier interaction, localization, incorrect repair, efficiency…)
organize evidence but *prescribe no remedy*; the evolver owns the causal claim.
Misattribution then just costs one iteration because edits must predict their effect
and get reverted when the prediction fails.

**First-person feedback** (HarnessCompass, https://arxiv.org/abs/2608.01918): ask the
task agent itself where it got stuck using the harness, then check each claim against
the trajectory and keep only what's supported. Raw traces show *that* and *where*; only
the agent knows *why*.

**Start minimal.** Both HarnessCompass and One Recipe start from a one-shell-tool seed
so every later component was introduced and measured by the loop, not inherited.

**Self-modification safety** (Ouroboros, https://arxiv.org/abs/2608.08311): if the
agent edits its own core, serialize through a reviewed-commit gate; every write
**invalidates prior review evidence** (freshness bound to the staged snapshot); keep
launcher/supervisor outside the mutable repo so the thing being improved can't improve
away its own kill switch.

---

## 3. Recovery: diagnose, then restrict

- **DARC** (Ant International, https://arxiv.org/abs/2608.11772): don't append a
  universal recovery context after every failure. Profile failures on a dev set →
  assign a dominant failure mode → restrict the admissible intervention set (action
  guard / API procedure source / induction rule / few-shot budget) → enumerate short
  ordered policies over that set → score success − λ·cost → freeze the winner. Routing
  among *interventions*, not models. Diagnosis-matched small policy space ≈ full-library
  cascade at much lower cost.
- **Deterministic stuck signals** (PMCoder, https://arxiv.org/abs/2608.06811):
  persistently nonzero exit codes, one file edited too many times, reads saturated
  without edits, one normalized action repeated past threshold. Cheap harness-side
  detectors, no LLM needed. On stuck: mark subtask failed, push recovery subtasks —
  notably **revert-then-refix** (restore edited files, re-fix from clean base).
- **Phase hysteresis** (PMCoder, https://arxiv.org/abs/2608.06811): forward phase
  transitions accepted immediately; backward/lateral ones require repeated evidence.
  Prevents thrash.
- **Reversible trajectory tree** (LoongReflect, https://arxiv.org/abs/2608.11967):
  failed branches are archived but excluded from active context; `<reflect>` emits a
  structured diagnosis (evidence, risk, turn-point, control intent ∈ {continue,
  backtrack}) — reflection as *state-control diagnosis*, not generic critique;
  `<backtrack>` rolls to a trustworthy prefix. Clean answer to "how do I let an agent
  undo without poisoning its context."

---

## 4. External memory: what actually works

- **BPE abstraction** (EvoHarness-RL, https://arxiv.org/abs/2608.05446): all harness
  external state reduces to three functional roles — **Belief** (what's currently true
  in the environment), **Progress** (subgoal statuses: attempted/open/blocked),
  **Experience** (cross-episode skills/failure-modes/priors). Four meta-actions:
  `track`, `commit`, `recall`, `note`. Crucially, harness actions **share the
  interaction budget** with environment actions, so the agent learns when bookkeeping
  is worth its cost. Cleanest minimal API for agent-managed state.
- **No universal memory structure; select per query.** MESA
  (https://arxiv.org/abs/2608.10108) swept all subsets of 5 structures (summary,
  temporal store, KG, raw episodic, vector DB): intermediate subsets beat both
  route-to-one and read-all, and no fixed subset wins across domains. Learn a cheap
  query→subset selector.
- **Consolidate offline, at idle ("dreaming")**, not at capacity limits. MindMemOS
  (https://arxiv.org/abs/2608.12428): entity-centered clusters, detect-then-act,
  conservative mutations, provenance links. Sleeping Agent
  (https://arxiv.org/abs/2608.11775): salience-score chunks into verbatim / gist /
  discard; key measurement — **temporal expressions survive gist compression at ~3% vs
  ~8% for entities**, so if you summarize, special-case time.
- **Ground memory in execution, not narration** (PMCoder,
  https://arxiv.org/abs/2608.06811): memory nodes record what commands actually ran
  and which files they touched; injected context mutates the live conversation but
  *never the memory graph* — retrieval cannot re-ingest its own output. Kills a whole
  class of self-amplifying errors.
- **Capacity cap + cross-instance-only admission** (One Recipe,
  https://arxiv.org/abs/2608.10178): memory admits only rules marked reusable across
  instances, and the cap pressures distillation rather than accumulation.
- **Associative recollection** (RippleMem, https://arxiv.org/abs/2608.13334): treat
  retrieval as *evidence completion* — recalled items serve as cues to hop to missing
  support, not one-shot top-k.

---

## 5. Skills: useful, and the failure modes to design against

- **Progressive disclosure** (Skill-Use, https://arxiv.org/abs/2608.04828): agent sees
  only name + short description; must retrieve the full procedure before following it.
  Right loading protocol; separates trigger (did it invoke?) from compliance (did it
  follow?).
- From "Agent Skills Can Be Harmful" (https://arxiv.org/abs/2608.11888): skills are
  *context that can actively regress you*, in two measurable ways: functional failures
  (no-skill passes, skill fails; or matched other-skill passes) and **efficiency
  regressions** (both pass but token/time ≥2×). Practical takeaway: any skill library
  needs a with/no-skill and cross-skill audit loop, and efficiency tracking, before you
  trust a skill's score. Also: description text is the routing surface — a
  competing/malicious description can hijack selection (Convergent Detour Hijacking,
  https://arxiv.org/abs/2608.12273), so treat descriptions as adversarial input.
- **Skill as optimization variable** (SKILLER, https://arxiv.org/abs/2608.10538):
  frontier model as critic converts execution evidence into *causal, localized*
  feedback; an actor applies *bounded edits* to the skill text; replay memory stores
  failure signatures + which modifications worked. Text policy optimization with no
  gradient updates — cheap and directly applicable to improving your own harness's
  skill docs.

---

## 6. Context economics (cheap, high leverage)

Prompt-Induced Waste (https://arxiv.org/abs/2608.01347) measured what harness prompts
actually do:

- Phrases **authorize work**: "consider multiple approaches" → 2.4–7.4× reasoning
  tokens, +3.5 approaches elaborated, exactly 1 implemented — the losing branches are
  pure token-borne tournament that never reaches the repo. "Maximum certainty" →
  re-verification loops of already-established facts. If the task reward doesn't pay
  for that work, don't word it in.
- A **bounded-efficiency instruction** ("begin with the failing test and likely files;
  inspect more only when evidence requires; smallest sufficient change; stop when
  acceptance criteria pass") was neutral-or-better on all 6 models, with no loss of
  diagnosis or validation. Free win.
- Models filter generic noise well but **plausible wrong hints cost 2.61×** reasoning
  and ambiguous scope costs both success and tokens. Sanitize context aggressively
  against *plausible* misdirection, not just noise.
- And the scaffolding study (https://arxiv.org/abs/2608.08654): cost differences across
  scaffoldings dwarf MCP-vs-CLI interface differences; input tokens dominate cost.
  Choose the loop, not the protocol.

---

## 7. Verification & trust plumbing

- **Evidence-gated completion**: accept "done" only against hard evidence (execution
  output, host-run verification commands), never model reasoning
  (https://arxiv.org/abs/2608.11274; LongHorizon-Harness's auditor is the same idea
  operationally, https://arxiv.org/abs/2608.01964).
- **Rubric-based per-task judges** (Faraday, https://arxiv.org/abs/2608.13331):
  auto-generate a task-specific rubric, judge against it — much higher human agreement
  and lower noise than a generic judge prompt. The reward-signal trick if you ever
  RL-train agents.
- **Differential testing against a trusted oracle** (ETH network verifiers,
  https://arxiv.org/abs/2608.11340): every disagreement between your model/verifier
  and an oracle becomes a counterexample driving the next repair; tests must co-evolve
  with the model or they stop biting. Generalizes to any harness self-improvement loop:
  you need an oracle, and your eval suite must grow.
- **Resume semantics** (https://arxiv.org/abs/2608.03836): if your harness persists
  workflows, the contract that matters is consume-once interrupts, schema-valid
  checkpoints, recovery as a pure function of durable state. They machine-checked real
  frameworks (CrewAI et al.) and found violations.

---

## 8. If you're also training

- **Coding agent as a tool** (Faraday, https://arxiv.org/abs/2608.13331): the outer
  agent invokes a frontier coding agent via shell with per-request deadlines, resumable
  sessions, parallel instances. Sub-agent orchestration without a framework.
- **World models to break the sandbox bottleneck** (WMRL,
  https://arxiv.org/abs/2608.12564): in agent RL, generation batches but execution
  doesn't — each rollout occupies a real sandbox. Replace execution with a learned
  outcome predictor, keep ~10% real "anchor" rollouts, debias with isotonic regression,
  fuse gradients inverse-variance. General lesson: cache/learn the expensive
  environment, keep a thin ground-truth stream.
- **Verifier-bounded credit assignment** (CREST, https://arxiv.org/abs/2608.13179):
  verifier sets gradient *direction* per turn; a privileged teacher only modulates
  *magnitude* per token. Ceiling stays verifier-bounded while signal gets dense.
- **RL-train narrow submodules** (CodeGrep, https://arxiv.org/abs/2608.05886): a 14B
  retrieval agent trained with GRPO, injected in front of a frozen main agent. Label
  trick: relevance = files past agents *actually reasoned from* (mined from
  trajectories), not gold-patch files — behavioral labels beat static ground truth.

---

## What I'd actually take, ranked

If building a general harness tomorrow:

1. **MEA-style rounds**: external verified task state + fresh-context executor +
   read-only auditor. Biggest structural win against long-horizon drift.
   (https://arxiv.org/abs/2608.01964)
2. **The four lifecycle hooks** as your harness's internal API — it's simultaneously
   where recovery lives, where guardrails live, and where automated evolution edits.
   (https://arxiv.org/abs/2608.02276)
3. **BPE external state + track/commit/recall/note**, budget-shared with environment
   actions. (https://arxiv.org/abs/2608.05446)
4. **Deterministic stuck signals + revert-then-refix + phase hysteresis** for recovery
   before you try anything LLM-driven. (https://arxiv.org/abs/2608.06811)
5. **Typed failure attribution (verifier-side + budget-side) with predict-and-revert**
   as the discipline for any harness improvement, human or automated.
   (https://arxiv.org/abs/2608.10178)
6. **Generalization gate + capability/guidance separation** the moment anything
   auto-edits the harness. (https://arxiv.org/abs/2608.01918)
7. **Bounded-efficiency prompt wording; strip work-authorizing phrases you don't pay
   for.** Free. (https://arxiv.org/abs/2608.01347)
8. **Progressive-disclosure skills + with/no-skill efficiency auditing.**
   (https://arxiv.org/abs/2608.04828, https://arxiv.org/abs/2608.11888)
9. **Idle-time memory consolidation with provenance; never let retrieval write back
   into memory.** (https://arxiv.org/abs/2608.12428, https://arxiv.org/abs/2608.06811)
10. **Threshold-based termination + two-tier goal contract** for anything user-facing.
    (https://arxiv.org/abs/2608.05144)

---

## Full paper index

| Paper | arXiv | Group |
|---|---|---|
| LongHorizon-Harness (MEA rounds) | https://arxiv.org/abs/2608.01964 | Alibaba (XiangXiang Chu) |
| Argus (runtime, thresholds, goal contract) | https://arxiv.org/abs/2608.05144 | HKU (Fan Yang) et al. |
| Harness-R1 (lifecycle hooks, RL engineer) | https://arxiv.org/abs/2608.02276 | SJTU (Weinan Zhang, Weiwen Liu) |
| HarnessCompass (generalization gate, tracks) | https://arxiv.org/abs/2608.01918 | Amazon (Yan Xu) |
| One Recipe, Many Harnesses (TRIAGE, slots) | https://arxiv.org/abs/2608.10178 | UIUC (Yu-Xiong Wang) + IBM |
| EvoHarness-RL (BPE abstraction) | https://arxiv.org/abs/2608.05446 | Multi-inst. (LLA@COLM 2026) |
| Ouroboros (self-developing harness) | https://arxiv.org/abs/2608.08311 | U. Louisville (Yampolskiy) et al. |
| HSI (hierarchical self-improvement) | https://arxiv.org/abs/2608.08466 | HKUST (Tailin Zhou) |
| Evo-Bench (harness-evolution benchmark) | https://arxiv.org/abs/2608.09096 | RUC (Wayne Xin Zhao) + Meituan |
| HarnessOpt-Bench | https://arxiv.org/abs/2608.06301 | Cambridge (Yuan Xue) / Scale AI |
| DARC (diagnosis-guided recovery) | https://arxiv.org/abs/2608.11772 | Ant International |
| PMCoder (stuck signals, phase planner) | https://arxiv.org/abs/2608.06811 | Vanderbilt (Yu Huang) |
| LoongReflect (trajectory tree, backtrack) | https://arxiv.org/abs/2608.11967 | Peking U. et al. |
| MESA (memory structure selection) | https://arxiv.org/abs/2608.10108 | — |
| MindMemOS (dreaming consolidation) | https://arxiv.org/abs/2608.12428 | MindMemOS Team |
| Sleeping Agent (salience consolidation) | https://arxiv.org/abs/2608.11775 | — |
| RippleMem (associative recollection) | https://arxiv.org/abs/2608.13334 | — |
| LycheeMemory (segment consolidation) | https://arxiv.org/abs/2608.12990 | Lychee Team |
| Skill-Use (progressive disclosure) | https://arxiv.org/abs/2608.04828 | Fudan (Yanghua Xiao) |
| Agent Skills Can Be Harmful | https://arxiv.org/abs/2608.11888 | HUST + MSR + UIUC (Tianyin Xu, Fan Yang) |
| Convergent Detour Hijacking | https://arxiv.org/abs/2608.12273 | CUHK et al. |
| SKILLER (skill as optimization variable) | https://arxiv.org/abs/2608.10538 | Shanghai Jiao Tong U. et al. |
| SKT (verified skill-use training data) | https://arxiv.org/abs/2608.02287 | Shanghai AI Lab |
| Prompt-Induced Waste | https://arxiv.org/abs/2608.01347 | PointFive |
| MCP vs CLI scaffolding study | https://arxiv.org/abs/2608.08654 | UPC / U. Salamanca |
| Agent Safety as Runtime Contract | https://arxiv.org/abs/2608.11274 | — |
| Training AI Scientists (Faraday) | https://arxiv.org/abs/2608.13331 | Microsoft Research + Louis Kirsch |
| Self-evolving network verifiers | https://arxiv.org/abs/2608.11340 | ETH Zürich (Laurent Vanbever) |
| Resume Contract (workflow persistence) | https://arxiv.org/abs/2608.03836 | Sajjad Khan |
| WMRL (world models for agent RL) | https://arxiv.org/abs/2608.12564 | UIUC (Jingrui He) + Amazon |
| CREST (verifier-bounded credit assignment) | https://arxiv.org/abs/2608.13179 | Zhejiang U. et al. |
| CodeGrep (RL retrieval submodule) | https://arxiv.org/abs/2608.05886 | — |
| SSPO (step-level self-distillation) | https://arxiv.org/abs/2608.12764 | — |
| Self-Evolving Defense (HARD) | https://arxiv.org/abs/2608.12977 | — |
| SHAPER (skill-harness co-evolution, embodied) | https://arxiv.org/abs/2608.11350 | Northeastern + MSR |
| CAKE (kernel + compiler-harness co-evolution) | https://arxiv.org/abs/2608.12629 | NVIDIA + CMU |
| AutoDesign (meta-harness optimization) | https://arxiv.org/abs/2608.13560 | — |
| Beyond Retrieval (trajectory rebinding) | https://arxiv.org/abs/2608.12847 | — |
| Coderlet (request lifecycle) | https://arxiv.org/abs/2608.09480 | Mengfan Li |
| Simulator Collapse in MARL | https://arxiv.org/abs/2608.12253 | Berkeley (Levine) + Stanford (Manning) |
