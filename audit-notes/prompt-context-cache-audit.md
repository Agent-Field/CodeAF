# Prompt architecture, context management, and cache audit — Aug 2026

Two read-only audits over the live code and real run evidence: 20 leaf streams
(332 turns, 4.06M input tokens) recovered from `~/.aforge/workspace/*/.obs`
traces, plus the `usage` journal in `~/.aforge/graph.db` (155 rows). This file
is the canonical record; the fix waves reference finding ids.

## Verdicts

**Prompts: no new skeleton.** One already exists and traces show it filled:
brief owns the objective and done-means, contract owns verify/pitfalls/stuck-
route, code owns inputs. The defects are two slots with no single owner and one
prompt that is wrong about the machine it writes for.

**Context/cache: not near-ideal — two wiring errors and two miscalibrated
constants, not a bad design.** The recurring conceptual error, made in five
places and diagnosed-but-not-generalized once (`toolbelt.go:1583`), is placing
prompt blocks by semantic category when cache position is governed by
**volatility**. And every cache-shape discipline in the codebase serves a cache
the biggest caller never requests: `provider.WithCacheKey` is set on exactly
one path, while the head — 47.5% of prompt tokens, 87.7% of cost — never asks.

## Prompt findings (P)

- **P1 — delivery law, five authors, one carve-out.** `contract.go:188` says
  write the whole deliverable in the final message and never file it;
  `linear.go:147` says keep it under ~300 words and file the long version; only
  the gate (`chat.go:4186`) knows file-shaped asks are the exception — and
  `chat.go:4190` makes the contract binding over the carve-out. Task-304 died
  over four gate rounds; round 3 filed the 37KB deliverable and was failed for
  obeying the sane rule. No prompt knows a deliverable has a size. Fix: one
  shared delivery-law constant carrying the carve-out, driven by the
  file-shaped bit `leafOutputHint` (`chat.go:1622`) already computes.
- **P2 — `contract.go:28` believes the worker has four tools.** Actual toolbox:
  sh, job, write, edit, web, recall, share, capabilities → media/document
  tools. The one prompt whose job is the working method cannot route through
  media, documents, background jobs, siblings, or memory. One sentence.
- **P3 — `OverrunGoal` nests itself** (`overrun.go:69`): each repair round
  wraps the previous wrapper; measured briefs carry three verbatim copies of a
  paragraph addressed to a planner, handed to a worker. Fix: strip the fixed
  prefix before re-wrapping; phrase the header for its reader.
- **P4 — done-means has two unreconciled authors.** The contract sees the brief
  but is never told its acceptance bar is binding; task-601's contract verified
  structure when the brief's bar was flicker (task-670 got it right — coin
  flip). Fix: one clause linking contract verify to the brief's bar.
- **P5 — "checking is part of doing" never reaches `revisePrompt`** — the
  doctrine gap behind the 27-round spiral the governors only bound. Fix:
  extract `checkingRule` from `proportionRule` (byte-preserving concat), add to
  revise and overrun.
- **P6 — worker premise restated in prose six times**, one wrong (P2). Single-
  source as `workerPremise` beside `agentPremise`.
- Correctly single-sourced today: `agentPremise`. Correctly specialized, do not
  merge: the anti-escalation rule (3 agreeing copies), finished-not-merely-
  written (3 deliberate voices).

## Context/cache findings (F)

- **F1 — cache key set on one path only** (`chat.go:758`). Head router, belt
  loop, all planning calls, and the whole `aforge run` path never set it, so
  affinity and `prompt_cache_key` are absent where 87.7% of cost lives. Fix:
  `provider.WithCacheKey` at each loop entry (session id / goal id / run id).
  ~4 lines. Paired with F0 this is worth roughly ~58% of the head's prompt
  bill; unpaired, ~46% cacheable instead of ~65%.
- **F0 — live measured counters on system prompts** (`compiler.go:324`,
  `chat.go:4742,4861`; churn from `subharness.go:226` + `selfknow.go:103`,
  and `subharnessKnowledge` has no TTL), and measured execution history at
  head block 1 (`head.go:803`) — stable between jobs, churning during one.
  Fix: measured blocks move to the user message below stable material; TTL.
- **F2 — `cached_tokens` computed three times, persisted nowhere.**
  `store.NodeUsage` and the `usage` table have no column; the tracer writes
  only `in=/out=`. F1 survived unnoticed because of this. Fix: column +
  `cached=` on the trace turn line.
- **F3 — decay hysteresis defeated by its own arithmetic.** `obsBudget =
  maxTokens/6` (a run-spend number, a category error) → 25KB window, 6.25KB
  headroom vs measured 3.7KB median inflow → fires every ~1.7 turns, each fire
  ~4× negative (re-bills the tail cold). ~8% of leaf input. Fix: size window
  from model context length minus floor; low-water from measured inflow.
- **F4 — the identical-call memo catches nothing**: any successful `sh` clears
  it and 87% of turns contain `sh`; 27/27 observed duplicates crossed turns;
  12.5% of all observation bytes were fetched twice or thrice. When it does
  fire it re-inserts the full previous body. Fix: content-address results —
  hash every tool result, replace repeats with a pointer to the earlier copy.
  **F4b:** the `capabilities` tool description mutates on arming (extra,
  unintended invalidation on top of the costed schema append) — freeze it.
- **F5 — leaves are starved, not bloated.** The re-read subject (26KB) is
  larger than the whole 25KB window; the loop uses <10% of available context
  while paying 12.5% of reads twice. F3/F4/F5 are one defect from two ends.
  Leaf tool floor is genuinely clean — nothing rides every turn that
  shouldn't. Note: lossy `cappedOutput` (12KB) sits above lossless spill
  (10KB); ordering is backwards though unobserved in this corpus.
- **F6 — `revisionResultBytes=1200` clips the ~300-word deliverable the
  sentinel judges to ~60%, with no truncation marker.** Raise to ~2400 +
  marker.
- **F7 — head prompt: `renderServices` uncapped and appended after the graph
  budget is enforced; `renderThread` drops the newest messages (fills from the
  head against an 8KB cap); unbounded per-row brief/summary lines can void the
  snapshot; cent-resolution cost in the deep slice (`depth.go:131`) beside the
  block that already fixed exactly that; a second minute-clock at
  `head.go:1653`; live step count above the thread in revisionvoice.
- **F8 — belt prompt ordered by importance, not volatility** (board first,
  thread last). Select by priority, emit by volatility.
- Minor: the tracer writes into the leaf's own `.obs/`, and leaves were
  observed reading it — cross-leaf contamination by construction.

Checked and clean: no map iteration reaches any prompt repo-wide; the only
mid-transcript rewrite is the sanctioned decay pass; tool definitions
deterministic except F4b; nowLine placement/flooring correct; VoicePrompt
stable; thread folding append-shaped; encodeRequest deterministic.

## Ownership

Chat session (internal/head, internal/store*, internal/tui, cmd/aforge/chat.go):
F0, F1 chat/head sites, F7, F8, P1's chat-side wiring. This session
(internal/plan, internal/exec, internal/resident): P1-P6 constants and prompt
edits, F1 run-path key, F2 (store/usage.go is outside the chat session's dirty
set — coordinate at merge), F3, F4, F4b, F5, F6, tracer relocation.
