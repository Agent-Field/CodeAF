# Agentic Harness Research Notes — Set 2 (arXiv, wider sweep)

Second sweep, covering harness/agentic techniques from late 2025–Aug 2026, from
reputable groups (Berkeley, Stanford, MSR, IBM, Tencent, Duke, NUS, Fujitsu,
Ant, Cambridge/Scale, Ant, Salesforce, UChicago, HKUST). All preprints,
unreviewed; mechanics over benchmark claims.

---

## 0. The decomposition every harness should have

Several independent reputable systems converge on **exactly the same harness
slot decomposition**. If you design slots once, design them as:

| Slot | What it is | Evolved how |
|---|---|---|
| **prompt / guidance** | system prompt, tool descriptions, rules | guidance edits, prompt layer |
| **workflow / hooks** | state-dependent intervention points | adaptive hooks/triggers |
| **tool implementations / skills** | executable capability | capability edits (code) |
| **lessons / memory** | cross-instance reusable rules | distillation, capped admission |

Sources — powers this decomposition twice-over:
- EvoHarness-RL [2608.05446]: exactly this as the *policy-facing* abstraction, and the **BPE** triad (Belief/Progress/Experience) + `track/commit/recall/note` meta-actions (the 4 hook verbs). So the slots aren't arbitrary: they're exactly what a trainable agent needs to *read* and *write*. — https://arxiv.org/abs/2608.05446
- "One Recipe, Many Harnesses" [2608.10178] (UIUC+IBM): the same slots as a *plugin* the evolver edits — and their key empirical result: **gains come from compensating recoverable execution defects, not raising the capability ceiling.** — https://arxiv.org/abs/2608.10178
- HarnessCompass [2608.01918] (Amazon): **structural vs guidance** split, generalized, evolves them on separate tracks and **merges** survivors (not joint edits). — https://arxiv.org/abs/2608.01918
- Ouroboros [2608.08311]: same decomposition + a **reviewed-commit gate** so the thing being improved can't improve away its own kill switch; fresheness bound to the staged snapshot; writes invalidate prior review evidence. — https://arxiv.org/abs/2608.08311

So: build the harness as a **plugin loop around a frozen model** with four
slots and a review gate; the agent *both* reads the slots (via the 4 meta-actions)
*and can be evolved by the same slots*.

---

## 1. Recursive Theorem: model–harness co-evolution is the actual scaling law

**Recursive Harness Self-Improvement (RHI)** — Hyunin Lee, Matei Zaharia (Berkeley), Yujin Tang, et al.
- arXiv:2607.15524 — under author names.
- The framing: AI progress increasingly comes from **co-evolution of foundation model and harness**, not model alone: stronger harness → better workflows → higher-quality execution traces → these traces train the *next* model.
- Their algorithm: a "harness optimizer" that, given tasks + a policy, proposes a harness, run, evaluate, iterate few-shot with an LLM-as-judge over exactly the four slots.
- Findings (eph): **few-shot RHI lifts the ceiling of test-time scaling** (not just the slope); gains are *not* explained by more output tokens; and it *reduces* cache read/write cost. Ablation on the implicit objective: it improves **f_ext** (external trace quality ✓ better than just token count).

**Takeaway**: build the harness so it *generates better execution traces* — the trace is the real artifact. Everything below (state separation, verified memory, recovery) is in service of making the trace teachable.

---

## 3. Turn-level credit assignment & reward shaping (for RL)

- **CREST** [2608.13179] (Zhejiang U. + Shanghai Innovation Institute): verifier-bounded credit assignment — **per-turn** (which turn contributed success/failure verdict-reward; gradient direction) × **intra-turn entropy-gated self-teacher** (magnitude only). The ceiling stays verifier-bounded; dense signal without reward hacking. — https://arxiv.org/abs/2608.13179
- **ABSeeker** [2608.05102] (Ant Group): **answer-backtracked clue recovery**: given query + verified answer, recover a set of intermediate "clues" (evidence) that define meaningful progress; then score every step against the clue set → **dense step-level rewards** for GRPO + step-weighted SFT. Fixes the two credit-assignment failures: correct final answer with flawed steps, and wrong final with useful intermediate steps. — https://arxiv.org/abs/2608.05102
- **CRISP** [2608.01867] (V2): **critical-step perception**: a *strong teacher* annotates which steps carry evidence for the final answer via *backward evidence induction* (judge each step against future-confirmed evidence); a small student then labels criticality, and the RL reward rewards trajectories with higher critical-step proportion → efficiency-aware agents. — https://arxiv.org/abs/2608.01867
- **Arbor** [2606.03239]: **reusable rubric buffer** — natural-language process rubrics shared across queries, induced contrastively (positive anchor / worst negative / hard negative), admitted/consolidated/retired online to shape agent RL without per-query reward engineering. — https://arxiv.org/abs/2606.03239
- **LongTraceRL** [2605.31584] (Tsinghua): composite **outcome + rubric** GRPO reward for long-context search. — https://arxiv.org/abs/2605.31584
- **OpenForge-RL** [2607.21557] (Microsoft Research, Columbia, Dartmouth): the *infrastructure* to do RL on real harnesses (Claude Code, OpenClaw, browser/computer agents): a proxy that serves harness model calls while recording them as training data, + k8s rollout containers. Finding: **some harnesses are much harder to RL-learn than others** — harness choice is a training-cost variable. — https://arxiv.org/abs/2607.21557

Takeaway: if training, use **verifier-bounded direction + per-step (not per-trajectory) granularity**, and treat **harness choice as a hyperparameter** for RL.

---

## 4. Context management & programmatic memory

**This was the deepest new vein.**

### 4a. Append-all, programmatic search (lossless) — PRO-LONG (Duke)
- arXiv:2607.20064 (Fox, Wang, Rosu, Dhingra @ Duke).
- **Write = harness appends every observation/action/outcome to a log; Read = programmatic search (regex / code) over that log**, not vector DB.
- Principles: **simplicity, losslessness, coding-agent-native retrieval**. On ARC-AGI-3, beats a base coding agent ~+18 pts across frontier models, matches SOTA specialized harnesses at **4.2–5.8× fewer tokens**; 97.4% best@2 with Fable 5 (cost claim).
- The insight: LLM context management = **decouple "accessed" (context window) from "accessible" (tool-reachable log)** and let the agent itself search history via code rather than a learned embedder/retrieval heuristic.

### 4b. Addressable Recall Compaction (ARC — Fujitsu)
- **https://arxiv.org/abs/2607.25066**.
- Modern agents die from context overflow; the four standard compaction paradigms (truncation, hierarchical summarization, state folding, RAG) are **all fundamentally lossy**.
- ARC approach: **keep every tool observation verbatim in an append-only store (lossless memory), never delete; in the live context use compact summary + short observation + citation hash (§a91f3c20 → full observation in store)**, and `_recall §id` retrieves on demand.
- Token-per-turn: by keeping only a bounded view + citations, std case stays *much* below the context limit; but exact recovery of any past observation is always possible.
- This is the engineering answer to "lossless + cheap": **lossless storage, lossy projection, recompute-on-demand**.

### 4c. **Twin Agent — context residual compression + privilege separation**
- **https://arxiv.org/abs/2607.19595** (Berkeley Hu/Wagner, UChicago Chen, UIUC Bo Li).
- Two agents: **Explore Agent** (reads untrusted info) and **Safe Agent** (executes privileged actions). Explore is *conditioned on the Safe Agent's current context* and communicates only **compact hints** (residual—not full observations) to Safe about next action. Math of "residual coding" → security-utility tradeoff.
- Evaluated SWE-bench Lite / AgentDojo / DecodingTrust-Agent: preserves utility while blocking prompt injection.
- Great for: any harness that ingests untrusted context (web, tool output, chat) — move "what to do next" synthesis into a low-privilege hop and pass only a hint up.

### 4d. **Context-aware memory dedup + salience-consolidation**
- **Sleeping Agent — salience-weighted consolidation** [2608.11775]: proactive scheduling (consolidate at idle, not capacity), score chunks (DownstreamSimilarity 0.4 / Recency 0.3 / InfoDensity 0.3) then high-verbatim/mid-gist/low-discard. Key measurement: **episodic temporal detail survives gist at ~3% vs entities ~8%** — so summaries specifically lose time. — https://arxiv.org/abs/2608.11775
- **MindMemOS — dream (offline consolidation)** [2608.12428]: entity-centered scope, detect-then-act, provenance-preserving, mark consumed; solve redundancy/conflict by **consolidate at idle, not at capacity**. — https://arxiv.org/abs/2608.12428
- "When Your Agent Opens the Chat App" [2608.12888]: **agent-controlled search over raw chat logs rivals structured memory** — another PRO-LONG-style "programmatic read over lossless log" for the *personal-memory* domain. — https://arxiv.org/abs/2608.12888

### 4e. **Explicit stances survive compression**
- **The Sleeping Agent** [2608.11775] + **"Explicit, Not Longer"** [2608.06953] (Kwon, independent):
  - Compression is built to drop qualifiers → a claim's *epistemic stance* (attribution, hedge, date) is exactly what dies in memory writes ("factwashing": 55% of hearsay-derived writes lose standing vs 7% business email).
  - Fix: **write stance as a labelled field, not a bracketed aside; make stance explicit, not merely longer.** +~15 points retention both models. Labeled-field > aside. (And "explicit not longer" — format wins over length.)
  - https://arxiv.org/abs/2608.06953

---

## 5. Self-correction, steering, and recovery

- **DARC** [2608.11772] (Ant International): **diagnose, then restrict, then freeze**: profile failure mode → restrict admissible recovery interventions → distill a cost-aware short policy → take it. Diagnosis that derives a *failure mode* wins over adding more recovery context. — https://arxiv.org/abs/2608.11772
- **One Reflection Is Not Enough — SAGE** [2606.31478] (MSR?): **multi-hypothesis failure attribution** — instead of a single free-form reflection, generate multiple competing causes, rank by severity (adversarial critic), check data-sufficiency, route to the exact intervention level (hypothesis / design / implementation) + **grounding manifest** (only claim what the measured pipeline actually produced). — https://arxiv.org/abs/2606.31478
- **LivePlan — Online Monitoring and Corrective Steering** [2608.06701] (IBM Research): a **Monitor** that watches the executing agent; a stronger **Advisor** that intervenes *only on* evidence of stalling (long stagnation in a phase, or behavioral drift) — the tested trend: *adaptive*, evidence-triggered steering (+11–15% success over vanilla) beats *periodic* advice, and predefined-advice (no advisor) is worst. **Intervene on failure, not on a schedule; cap repeated interventions.** — https://arxiv.org/abs/2608.06701
- **PMCoder** [2608.06811] (Vanderbilt): the deterministic-stuck-signals set (nonzero exit codes, file edited too many times, reads saturated, repeated action) + **revert-then-refix** + phase hysteresis (forward accepted immediately, backward needs repeated evidence) + **memory grounded in executed commands, not narration** (injected context never mutates the graph). — https://arxiv.org/abs/2608.06811
- **Experience Memory Graph (one-shot error correction)** [2607.13884]: convert exploration+expert trajectories into **action-decision graphs** (nodes=actions, edges=preceding observation) → graph-matching failed↔successful pairs extracts "under this observation, replace wrong action with X" corrections; retrieve at test time as **one-shot guidance without an extra loop**. — https://arxiv.org/abs/2607.13884
- **AgentDebugX** [2607.18754] (Kit, **portable attribution taxonomy + recovery as a rerun-able fix**; "the step where a failure becomes visible is often not the step that caused it") — deployable observability/attribution/recovery, MIT licensed. — https://arxiv.org/abs/2607.18754

---

## 6. Tool calling: the programmatic pivot

- **The Bitter Lesson of Tool Calling** [2608.06370] (unsure authors, but realistic): compares **programmatic tool calling** (agent writes a Python script importing typed stubs of the functions) vs. **JSON tool calling** (API JSON schemas). Programmatic path runs in a subprocess, printed stdout parsed; equal LLM-call count. No headline yet read fully — the **mechanism**: give the tool surface as *code, executed as code*
- **Qwen-CUA** [2608.02352] (Qwen team): agent-native computer-use model — programmable control vs. JSON; natively emits programs for GUI/computer use.
- **Faraday** [2608.13331] (MSR): **5 tools** (apply_patch / read_file / list_dir / grep_files / shell), **coding agent as a tool** invoked via shell with per-request deadlines and resumable sessions, linear append-only context, "maximally permissive" — and capability improved by weights not harness.

Mechanism worth stealing: **programmatic interfaces** — the tool surface as code the agent imports/executes instead of JSON schemas (less parsing fragility, more expressiveness), and **the tool docs as typed stubs.**

---

## 7. Context as the failure layer

- **TRACE** [2608.09153] (Salesforce? unclear; claims): frames **context engineering as a debug target**: mine historical agent trajectories for implicit dissatisfaction signals (corrections, abandonments), attribute failures to context *sources* (prompts, KB, downloads, memory) → recommend CREATE vs UPDATE vs DELETE. Their **exploratory verification** (>50 pts): the agent must actually *read* the context source file to decide whether content is missing or wrong.
  - https://arxiv.org/abs/2608.09153
- **AI Agents Do Not Fail Alone: The Context Fails First** [2607.14275], **Stop Shipping AI Agents on Faith** [2607.27677] (freelance/industry? under judge): positioning context as the primary failure layer. — https://arxiv.org/abs/2607.14275, https://arxiv.org/abs/2607.27677 (verify claims)
- **HANDBOOK.md** [2607.25398] (benchmark): long-context *agentic instruction-following* — rules must be used, not just present; a good harness measures instruction-following across the instruction surfaces (cf Harness-IF [2608.11727]).
- **Diagnosing Search Behavior** [2608.01913] (NUS/Tencent): behavioral taxonomies of search agents: incomplete (step cap / context overflow / tool error), over-search (repeated queries), wrong-answer-with-success; **effort/cost correlates**; per-turn query batching (Q/turn >1) as a cost lever. — https://arxiv.org/abs/2608.01913

---

## 8. Self-evolving skills & competence distillation

- **Skill Self-Play** [2607.22525] (proposer + solver co-evolve via skills): on models as small as Ministral/Granite, **co-evolving prose skills** as the *distillation* improves tool-call and multi-step by ~+40 pts vs unguided self-play — because skill-structured knowledge persists across generations where raw imitation collapses. Mechanism: a student generates procedural skills (natural-language), which are then used to guide new rollouts; only rollouts that pass a verifier train the weights. — https://arxiv.org/abs/2607.22525
- **FailForge** [2608.08570] (Shanghai AI Lab + ECNU): **distill procedural competence from persistently failed tasks**: diagnose the failed instances (beyond the teacher's frontier), **induce a skill, leakage-filter it**, re-rollout *with* the skill until success, then **remove the skill before fine-tuning** (so the student's test-time interface stays clean).
  - https://arxiv.org/abs/2608.08570
- **SKILLER** [2608.10538] (SJTU et al.): **skill as an optimization variable** — a critic turns execution evidence into causal, localized feedback; an actor edits the skill text; a replay memory keeps failure signatures + what worked; no gradient updates. — https://arxiv.org/abs/2608.10538
- **SkillCoach** [2607.01874] (user): self-evolving **trajectory-level rubrics** over four process dimensions (skill selection, following, composition, reflection) with a separate external verifier; evidence-grounded judging + arbitration patches + validation-gated admission, then rubric-filtered training.
  - https://arxiv.org/abs/2607.01874

---

## 9. Verification & safe-commit primitives

- **SafeCommit** [2608.04289]: certify "may act" under **memory uncertainty**: keep a calibrated set of plausible worlds; an action is certified only if safe in *all* retained worlds; else take a low-side-effect *probe* (permission check, metadata read, staged diff, simulation) expected to remove worlds; else defer / ask. The action-certification pattern for any memory-grounded agent. — https://arxiv.org/abs/2608.04289
- **LatticeMind** [2608.08236]: **conflict-aware memory**: agent writes typed *structured* claims (FACT / CONSTRAINT / SUB_PLAN with evidence); a deterministic symbolic gate resolves most conflicts by rules (evidence freshness, authority), and only unresolved semantic conflicts go to an LLM **reconciler**, which *keeps both claims* (winner confirmed, loser contested/superseded) rather than picking one answer.
  - https://arxiv.org/abs/2608.08236
- **AgenticRepair** [2607.29422] (IEEE TSE under review): **multi-faceted program context** = code-structure + runtime-execution + commit-history contexts, each engineered by a subagent, embedded into the repair agent's memory; +73% success on SEC-Bench (+29pp over baseline); facets complementary.
- **Resume Contract** [2608.03836]: if you persist workflows, the contract that matters: consume-once interrupts, schema-valid checkpoints, **recovery as a pure function of durable state**. — https://arxiv.org/abs/2608.03836

---

## 10. Decision/routing primitives

- **Monitor + Advisor steering** already above ([5D]). Key principle: **soft, reversible interventions beat hard blocks**; a "advisor stronger than executor" is a prerequisite.
- **SearchOS** (re: SearchOS-V1 [2607.15257], a collaboration): multi-agent search with a supervisor board, plan formation — a reference for agent **state machine + handoff** patterns.
- **JANUS** [2607.19913]: **foresight-guardrail**: a dual-task model identifies current risks and *anticipates future unsafe outcomes* from partial trajectories (risk taxonomy: user/environment/agent origins; simulation-built data).
- https://arxiv.org/abs/2607.19913

---

## Top picks (previous set + this one)

### — worth building into a harness *today* —

1. **Append-all log + programmatic read/recall (PRO-LONG / ARC)** — lossless, cheap, coding-agent-native. ([2607.20064], [2607.25066])
2. **Managed-state rounds: external verified task state + fresh executor + auditor** (MEA, LongHorizon-Harness) — anti-drift spine.
3. **Programmatic tool interface** (typed stubs + script execution / "tool as code") over JSON-schema tool-calling.
4. **Monitor–Advisor steering**: hard signals (stagnation, drift) + a stronger advisor, fewer interventions, cooling windows, escalation caps. ([2608.06701])
5. **Confidence-gated safe-commit + LatticeMind-style two-layer conflict resolution**, and labels in memory as first-class state. ([2608.04289], [2608.08236], [2608.06953])
6. **Failure attribution (typed) + multi-hypothesis routing + granular (step-wise) dense rewards** when you RL. ([2606.31478], [2608.05102], [2608.01867])
7. **Skill as a transferable, verifier-gated artifact**: self-play skills + FailForge-Leakage-filtered distillation — the *skill is the unit of learning*, not the token.
8. **Context engineering as explicit external surface** to debug/attribute (TRACE-style "read the source file before saying UPDATE"). ([2608.09153])
9. **Sane recovery**: revert-then-refix, phase hysteresis, minimum-step floor, one-shot decision-graph corrections. ([2608.06811], [2607.13884])

---

## Full index (Set 2 papers)

| (paper) arXiv | Group | Notes |
|---|---|---|
| Recursive Harness Self-Improvement | 2607.15524 | Berkeley (Zaharia) — model–harness co-evolution as target |
| OpenForge-RL | 2607.21557 | MSR + Columbia + Dartmouth — RL training on any harness; harness choice = hyperparam |
| LongHorizon-Harness | 2608.01964 | Alibaba — MEA / external verified task state |
| HarnessCompass | 2608.01918 | Amazon — structural/guidance tracks + generalization gate |
| One Recipe, Many Harnesses | 2608.10178 | UIUC + IBM — typed slots; ceiling vs gap |
| Harness-R1 | 2608.02276 | SJTU — lifecycle hooks (pre-decision/pre-action etc.) |
| EvoHarness-RL | 2608.05446 | BPE roles + track/commit/recall/note |
| PRO-LONG | 2607.20064 | Duke — append-all log, programmatic read |
| Addressable Recall Compaction | 2607.25066 | Fujitsu — lossless store + citation view |
| Twin Agent | 2607.19595 | Berkeley/UIUC — residual-context privilege separation |
| Sleeping Agent | 2608.11775 | — salience-weighted consolidation compression |
| MindMemOS | 2608.12428 | “dreaming” idle consolidation |
| Agent-Controlled Chat-Log Search | 2608.12888 | raw log ≈ structured memory |
| Explicit, Not Longer | 2608.06953 | — label-based stance in memory |
| LivePlan | 2608.06701 | IBM — Monitor–Advisor steering |
| Diagnosing Search | 2608.01913 | NUS/Tencent — search failure taxonomy |
| TRACE | 2608.09153 | context attribution (CREATE/UPDATE) |
| AgentDebugX | 2607.18754 | open-source trace attribution + recovery |
| Experience Memory Graph | 2607.13884 | one-shot decision-graph corrections |
| One Reflection Not Enough (SAGE) | 2606.31478 | multi-hypothesis failure attribution |
| FailForge | 2608.08570 | SIGAI reporting leakage-filtered skills |
| Skill Self-Play | 2607.22529 | co-evolving skills, big distillation wins |
| SKILLER | 2608.10538 | skill as first-class optimization variable |
| SkillCoach | 2607.01874 | self-evolving rubrics for skills |
| CREST | 2608.13179 | verifier-bounded per-turn credit |
| ABSeeker | 2608.05102 | answer-backtracked dense rewards |
| CRISP | 2608.01867 | critical-step perception for RL efficiency |
| ARBOR | 2606.03239 | reusable rubric buffer (online admission/retire) |
| LongTraceRL | 2605.31584 | outcome+rubric composite reward |
| Tool-Crit table: Bitter Lesson of Tool | 2606.06370 | programmatic vs JSON tool calls |
| FlowScout | 2608.10039 | execution-feedback workflow mining |
| SafeCommit | 2608.04289 | world-model safe-commit certificates |
| LatticeMind | 2608.08236 | conflict-aware memory (contested/superseded) |
| AgenticRepair | 2607.29422 | multi-facet context for vulnerability repair |
| JANUS | 2607.19913 | latent-risk foresight guardrail |
| Deep Research Pretraining | 2608.00432 | Tencent — predictive navigation from citations |
| Skill-Use | 2608.04828 | progressive disclosure |
| Agent Skills Can Be Harmful | 2608.11888 | with/no-skill audits |
| HandCode.md | 2607.25398 | long-context instruction benchmark |
| Stop Faith | 2607.27658 | 2607.14275 context-first philosophy |
| Coderlet | 2608.09480 | standard request lifecycle / clear harness loop |

(The full pre-existing set-1 doc is `harness-research-notes.md`; this is additive.)