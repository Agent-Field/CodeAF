# Product journey map — coverage, foundations and recommendations

Written 2026-09-14 from a read-only product and architecture audit of
`codex/personal-ai-backend` at `0ac146768`, plus the checkpoint-2 lane tree.
**This file is descriptive.** The only actionable checklist is
[NEXT-STEPS.md](NEXT-STEPS.md). Accepted behavior is in [DECISIONS.md](DECISIONS.md).
Everything under "recommendation" here is unconfirmed design advice. Dated receipts stay in
the BUILD-* records and are named, not copied.

The goal is every product journey (C17, C27). Backend work is judged by the journeys it
makes possible. The loop they share is: **talk naturally → retain useful understanding →
organize it → connect it to relevant work → act within instructions → explain the result.**

## 1. Where the loop stands

| Loop stage | Delivered and tested | Exists beneath, not yet a product step | Not built |
| --- | --- | --- | --- |
| Talk | Ordinary and unfiled chat; the shared chat opens as one history from two folders (J0) | — | Participation of work in a shared chat |
| Retain | Sourced informational `shared_context`; scoped holds; learned memory (dev) | `internal/direction`: one revisioned record with person receipts and a resolver, with no product caller | Keeping a clear instruction as governing direction without a save command (C09 capture) |
| Organize | Folders, filing ≠ placement, browsing TUI (J0) | Terminal and chat-tool filing; `stand` edit moves placement | Suggestion/correction flow, provenance of a filing, TUI management |
| Connect | Explicit governing placement; an explicit cross-folder file watch (scripted journey) | Rules-reaching reading; consumed-context records | Impact of a changed decision; consultation between efforts; discovery |
| Act | Terminal-created local-file ongoing work, live on DeepSeek V4 Flash (serial rates 7/10, 9/10, 10/10 across waves 3, 4 and round 3b) | Chat-card setup, edit and move (W5-B failed 21/37); cp2 bounded chat setup, pause, start again, edit and stop passed on GLM-5.3-Flash (lane, not integrated) | Work while away on a chosen background owner |
| Explain | Occurrence and receipt records; `context_trace` | cp2 inspector: last run, cause, rule id, receipt (lane, bounded acceptance passed) | Navigable chain from output back to the decision's source |

## 2. How retention is built today — four roles, one planned owner

Four places keep "understanding". They are **not four equivalent authorities**, and the
product needs them to stay distinct in role:

| Mechanism | Role today | Governs? |
| --- | --- | --- |
| Standing `hold` items (`internal/standing`, `WhenHold`) | The rule path that actually reaches chat, workers, scheduled runs and the report rules check | **Yes**, today's governing owner |
| `shared_context` (`internal/workspace/context.go`) | Sourced, revisioned information with explicit targets, injected as "not instructions or permission" | No, informational by law |
| Learned memory (`graph.db`, `internal/session/memory.go`) | Recall: preferences, facts and model-extracted decisions, ranked per turn; scope is a label, not a folder | No folder applicability; not a rule owner |
| `internal/direction` | The designed single record for rules, decisions and findings, with `PersonReceipt`-only acceptance, targets, exclusions, links and a resolver | Not yet: no non-test importer; BUILD-T03B's invalidation says the old stores still govern |

The fragmentation is architectural: an accepted instruction can be spelled in more than one
place and read differently. The existing T03b design already sets the migration: import
(lane 4), shadow (5), read cutover (6), write cutover with memory routing and C09 capture
(7a–7c), retire (8). Holds keep governing until the cutover, `shared_context` becomes
findings, and memory stays recall. This map does not reopen that plan or any confirmed
decision. It only places the lanes inside J3 and names what J4 needs from lane 6: runs
recording the direction revisions they consumed.

## 3. Coverage

### 3.1 The twelve gaps the user named

| # | Gap | Journey | Status | Beneath today | Missing |
| --- | --- | --- | --- | --- | --- |
| 1 | Natural automatic filing, suggestions, reversible associations, corrections | J2 | Backend partial | `collections` add/remove from chat and terminal; filing ≠ authority; unfiled chats | Suggestion or filing step in a turn (at `0ac146768` the writers found are the `collections` tool, the terminal and placement inheritance at setup; no suggestion flow was found in `internal/session` or `internal/tui3`, and none has been demonstrated); provenance and state on membership; undo; rejection survives rereads (C15/C16) |
| 2 | Self-managed folders — create, rename, file, move, share — vs a read-only view | J2, later stage (delete/archive) | Backend partial | Store and terminal create/rename/add/remove/place/unplace; chat create/add/remove/place; multi-membership works | TUI actions; chat rename; one-step move; delete/archive policy (unresolved, DT4); an explanation of sharing |
| 3 | Accumulated folder knowledge; the unified direction record with no caller | J3 | Backend partial | Holds scoped to folders; `shared_context` targeting folders; the direction library and resolver | T03b lanes 4–8; inspector for sourced rules and findings |
| 4 | Inspect, correct and withdraw memory, and see affected work | J3, J4 | Backend partial | Memory edit/forget/restore; context revise/withdraw; consumed-context records | Folder applicability for memory; a record of which work used a memory; affected-work view |
| 5 | A shared chat is placement, not dynamic participation | J5 | Not started (placement delivered in J0) | One history opened from two folders | Admission and departure of participants; governing context for a turn in a multiply placed chat; origin-labelled speakers |
| 6 | Global cross-work discovery | J5 (bounded), later stage (at scale) | Not started | `search_conversations` (lexical reading), `collections find` | A candidate signal without constant polling; relevance kept separate from authority (D08) |
| 7 | Dependency propagation; changed assumptions; stale outputs | J4 | Backend partial | Occurrences record the instructions version; consumed-context records; direction links carry `to_revision` | Consumed direction revisions per run (lane 6); derived staleness; maintenance authority (D04); running-work boundary (D03) |
| 8 | Optional folder purpose and activity rollup | Later stage | Not started | Collection name; inspector shows rules and parents | Purpose as optional description (never an automatic goal); bounded derived rollup |
| 9 | Dependable ongoing-work setup on open models | J1 | Working tree | Terminal-created journey passed live; cp2 bounded chat-card slice passed on open-weight models in the lane (BUILD-TUI-02) | W5-B blockers (loops, G2, driver); report rename; a profile open-weight allowlist; repeated open-weight pass rates |
| 10 | Rich combined triggers, multiple sources, continue vs new occurrence | Later stage (DT5 first) | Not started | One `When` kind per item: at, every, file, idle, probe, hold; recursive globs; braces refused | Multi-root watch; continuation and reporting policy (D14) |
| 11 | Reusable methods without leaking private inputs or widening permission | Later stage | Not started | Harness and subharness tools exist on the belt as separate machinery | Procedure separated from inputs and grants; run version and feedback (D09, D16) |
| 12 | A unified, navigable explanation chain across chats, decisions, work and outputs | J4 (J1 and J2 contribute) | Backend partial | Occurrence cause, receipts, `parent_cause` for standing runs, `context_trace`, cp2 "why" | Causes for tasks and forks (`not_recorded` today); links from a rule revision to its source; a TUI walk |

### 3.2 Journey and counterexample rows from PRODUCT-EXPERIENCE-PATH

| Row | Journey |
| --- | --- |
| Direct coding or research | Talk in every journey (C02); no filing required |
| Start in the wrong folder | J2 |
| Shared conversation | J0 (one history), J5 (participation) |
| Parallel related bugs | J5 |
| Recurring inbox or report | J1 (local files); later stage for continuation and adapters |
| Production monitoring | J6, later stage permissions and recovery |
| Decision changes mid-run | J4 (D03) |
| Completed output becomes stale | J4 |
| Wait for a person or event | J6 (held for the person); later stage durable waits |
| Stop during an external action | J6 (honest record), later stage recovery (D06) |
| Reuse a successful method | Later stage |
| Named roles and organization | Later stage identities (D12) |

### 3.3 The five acceptance journeys of C04

| C04 journey | Where it is exercised |
| --- | --- |
| Two bugs share one cause | J5 |
| API contract change across backend, frontend and docs | J3 (one sourced rule, several scoped consumers), J4 (impact) |
| Ongoing repository maintenance | J1, J6 |
| Research corrects a marketing campaign | J2 (correction), J3 (revision), J4 (outputs under the old assumption) |
| Travel information affects a calendar | J2 (C15 trip association and undo), J3 (explicit trip-specific instruction survives unlinking); calendar adapters are later stage |

### 3.4 Cross-cutting contracts

| Contract | Where it is settled |
| --- | --- |
| Provenance: who said, filed or found it | DT1 with J3 (direction receipts) |
| Explicit direction vs inferred proposal; which actions need a yes | DT2. C09 confirms retaining an explicit decision without a save; the external T03b design's §4.3 proposes treating a clear instruction in direct scope as the person's receipt |
| Model reliability | C28; each journey's serial open-weight acceptance with pass counts |
| Interruption, recovery, duplicate suppression | J6, J5, later stage recovery |
| Deletion and retention | DT4, later stage |
| Attention while away | J6, DT3 |
| Budgets and conflicts | Later stage (per-item limits and one live report owner exist today) |
| Permissions and resource binding | Later stage (D16) |
| Migration and testability | T03b lanes 4–8 (import, shadow, export); DT6 for untracked reds |

## 4. Foundations — recommendations, not confirmed

Keep the four presentation objects: Folder, Chat, Work, Artifact. Beneath them, reuse the
owners that exist and add properties, relationships and events before any new object.

| Concept | Recommendation |
| --- | --- |
| Folder | Existing collection. An optional purpose is a descriptive property, never Work. Activity is a derived, bounded projection |
| Filed in | Existing membership, **plus properties** origin, state (suggested / confirmed / rejected) and reason. A rejection is kept so an unchanged reread cannot restore it |
| Placed in | Existing governing placement. Written on explicit direction or inherited at setup, never inferred |
| Rule, decision, finding | The `direction` record, reached through the T03b lanes. `shared_context` becomes findings; memory stays recall |
| Applies to | Direction targets and exclusions (already modeled) |
| Execution | Existing occurrences and task runs, **plus a property**: the direction revisions consumed |
| Cause | An **event or edge** recorded at admission; `not_recorded` stays honest |
| Produced or used artifact | A relationship on the execution; staleness is derived from consumed revisions |
| Peer consultation | An origin-labelled message **event**; its result is a direction finding; a sustained exchange may become a Chat |

Recommended defaults for the internal design tasks in NEXT-STEPS:

- **DT1 association provenance.** Properties on membership, with the rejection pattern the
  direction store already uses. Add a separate table only if membership cannot carry it.
- **DT2 explicit vs inferred, and action classes.** A clear person instruction is its own
  receipt; no repeated yes cards for kept rules or routine corrections. Show a visible
  receipt with change and undo (the external T03b design's §4.3 proposal; C15's undo pattern). Inferred filings, extracted decisions and
  peer findings stay proposals, visible and reversible. Existing card behavior (stand
  propose/edit, place on explicit request) is unchanged until this is designed. Whether a
  chat stop is confirmed on a card stays the open owner decision recorded in W5-B.
- **DT3 work while away.** Reuse the existing background owner (standing timer or engine
  pass) for v3 items; keep v3 and resident surfaces and vocabulary separate; install nothing
  silently; say how checks happen (cp2 already does).
- **DT4 folder delete.** Archive (hide) rather than erase; contents keep their other
  memberships; removing a placement is said, because rules stop applying.
- **DT5 triggers.** A multi-root file watch before any Boolean composition, because W5-B's
  two-folder refusal is the observed need.
- **DT6 untracked reds.** A defect report for the `cmd/aforge-demo-home` failures and
  `TestTheOpeningHintNamesBothDoors`; the ledger itself only shrinks.

## 5. Model-validation limits

- Qualifying evidence must come from open-weight models (C28). Live evidence so far:
  - DeepSeek V4 Flash: wave-03 live6 passed on terminal-created items; W5-B live 1–5 failed
    as acceptance.
  - Checkpoint 2 exploration on the DeepSeek default route (resolved `deepseek-v4-flash-0731`)
    looped and never called `stand`.
  - Checkpoint 2 bounded acceptance on `z-ai/glm-5.3-flash` (Qwen3.8-27B rules check) passed
    once, phase by phase ([validation/tui-checkpoint-2.md](validation/tui-checkpoint-2.md)).
    One run is not a pass rate; setup needed one refused `stand` call and one stalled stream.
  - Closed-model exploration (Haiku) is historical only.
- A handful of samples is not a pass rate. The external T03b design asks N ≥ 10 for its
  live journeys, as waves 3–4 and round 3b ran for the terminal-created journey. Other journeys state their sample count in the acceptance contract before
  running.
- The steps that depend on a model's judgment carry the risk and have no measured
  open-weight reliability yet:
  - recognizing an explicit decision;
  - proposing a filing;
  - choosing a report path;
  - judging a probe;
  - the rules check.
- Each needs a deterministic receipt and a visible fallback.

## 6. Record of this map

- 2026-09-14: written with the journey-driven NEXT-STEPS. The previous checklist moved
  unedited to [NEXT-STEPS-HISTORY.md](NEXT-STEPS-HISTORY.md). Sources: DECISIONS,
  PRODUCT-EXPERIENCE-PATH, BUILD-TUI-01, BUILD-TUI-02 (lane branch only), BUILD-WAVE-03/04/05B,
  BUILD-ROUND-3B, BUILD-T03B, the external T03b design §4 and §6 (not in this repository), MEMORY-AND-CONTEXT-REVIEW, and code owners in `internal/workspace`,
  `internal/workspaceview`, `internal/direction`, `internal/standing` and
  `internal/session`. No build, test or model call was made for it.
