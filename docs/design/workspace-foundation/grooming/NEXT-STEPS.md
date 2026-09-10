# Working checklist

Updated 2026-09-10. This is the single execution checklist for the current review
and implementation preparation. [DECISIONS.md](DECISIONS.md) remains the authority
for accepted behavior; [CRITICAL-REVIEW.md](CRITICAL-REVIEW.md) contains the evidence.
A checked item means completed with the evidence named here, not merely discussed.

## Completed

- [x] Consolidate architecture, persistence and seven operating sequences.
- [x] Walk through 23 selected user journeys and counterexamples.
- [x] Critically review the architecture and trace backend draft #662 at
  `c63e03b7fe8ee84b6b94befa5403002593af0ebe`.
- [x] Run existing workspace/workspaceview/session/standing package suites on Spark:
  job `20260910-150118-000406` passed.
- [x] Reproduce concurrent stop/pause overwrite on Spark:
  job `20260910-150249-000407` failed as documented. **The defect is not fixed.**

## Current: settle decisions and choose the baseline

- [x] T01a — Complete two independent baseline reviews, including an isolated
  merge-tree simulation. Findings and differing recommendations are recorded below.
- [x] T01b — Baseline route accepted: preserve the draft and integrate dev in an
  isolated candidate. Implementation is in progress; parent draft remains intact.
- [x] T02a / D11 — Typed backend entities and composable components accepted (C19).
- [ ] T02b / D12 — Independent persistent identities remain to settle.
- [ ] T03 — Settle accepted direction, governing scope and conflicts (D01/D02).
  Preserve C09/C15/C16: explicit decisions are retained; references do not move work;
  actual moves change folder-scoped guidance for future work; exceptions are narrow.
- [ ] T04 — Settle work/run/control transitions and in-flight changes (D03/D06/D13).
- [ ] T05 — Settle activation, recurrence, continuation and reporting (D05/D14).
- [ ] T06 — Settle coordination, dependencies and completed-output maintenance
  (D04/D07/D08/D15).
- [ ] T07 — Settle execution identity, access/resource isolation and reusable
  configuration (D09/D12/D16).
- [ ] T08 — Settle persistence/recovery and extension boundaries (D11/D17/D18).
- [x] T09a — Record the bounded [first build contract](BUILD-WAVE-01.md): baseline
  integration and safe standing control persistence.
- [ ] T09b — Expand contracts as subsequent architecture decisions are settled.

## Subsequent delivery

- [ ] T10 — IN PROGRESS: integration subagent owns isolated candidate, retains
  draft ancestry and reconciles newer dev changes. Preserve parent #662.
- [ ] T11a — IN PROGRESS: lifecycle implementation subagent fixes stale writeback.
- [ ] T11b — IN PROGRESS: independent validation subagent writes functional
  concurrency/next-occurrence checks. Root validates the combined candidate on Spark.
- [ ] T11c — Broader admission/cancellation/recovery contracts remain future work.
- [ ] T12 — Unify governing context across chat, workers, independent checking and
  scheduled runs, with exact sources and revision-aware effective inputs.
- [ ] T13 — Complete the daily Slack journey, including setup, delivery, edits,
  unanswered questions, pause/stop and restart.
- [ ] T14 — Complete cross-work Product-to-Marketing impact, including worker
  publication, duplicate suppression, authority and no-manual-reopen activation.
- [ ] T15 — Extend proven seams to persistent roles, handoffs, reusable profiles,
  additional adapters and compound triggers; tick each supported journey only
  against its own real acceptance evidence.

For each delivery item, record the exact candidate revision, Spark job, result,
remaining failures and integration destination. Passing package tests is not
passing model/connector acceptance. Existing draft/manual/release restrictions
remain in force; no merge, release or replacement branch has been performed here.

## Decision session order

| First settle | Concrete decision | Recommendation, pending confirmation |
| --- | --- | --- |
| Backend building blocks | Is everything an object, or are there distinct entity/component/value contracts? | Typed entities, owned versioned components, small values and typed relations. Shared infrastructure; no universal property database. |
| Independent identity | Does a named assistant survive deletion or handoff of one of its duties? | Yes, when it spans duties: supporting identity separate from profile, Work, Chat and process. Ordinary work needs no named identity setup. |
| Direction and scope | What makes retained text govern an action, and what happens when rules conflict? | Source-backed adoption with explicit applicability; preserve compatible rules, flag unresolved conflicts. No model-written “accepted” flag as authority. |
| Work lifecycle | What exactly do edit, pause, stop, finish and resume change? | Separate intent/control revisions, attempts, waits and receipts. Late completion cannot revive stopped work. |
| Activation and reporting | When do events start a new occurrence versus resume one? | Explicit occurrence/continuation identities, no overlapping unfinished work by default, reporting policy distinct from change alerts. |
| Collaboration | When does a finding inform, consult, activate or require a new decision? | Existing authority bounds automatic action; dependencies propagate reliably, semantic discovery is measured best effort. |
| Isolation and reuse | Which context, account, tools and resources does execution get? | Narrow effective capabilities, explicit bindings, versioned profiles; copies do not copy private state or grants. |
| Storage and evolution | What is authoritative, what is rebuildable, and how do new capabilities fit? | Existing lifecycle owners, durable change/admission contracts, reconstructible indices and typed adapter contracts. Add a new store only for a demonstrated transactional need. |

User-facing names and presentation remain separate and open. This agenda does not
reopen confirmed product behavior merely because its implementation is unsettled.

## Baseline review — completed, route pending

The user explicitly requested both subagent reviews. The source-retention reviewer
favored a fresh dev candidate with a selective final patch; the integration reviewer
favored preserving draft ancestry and integrating dev. Both agree that rewriting
from scratch would discard useful tested code without solving the architectural
gaps. Their disagreement is about integration risk and review granularity.

| Evidence | Result |
| --- | --- |
| Backend draft | #662, `c63e03b7fe8ee84b6b94befa5403002593af0ebe` |
| Reviewed dev | `9961173140ba24ff99ef91c8e93fb79e326d854b` |
| Common ancestor | `4737e7f1e9b81a92ccefd8352f3dd0b6b289bc1e` |
| Divergence | 23 draft-only commits; 47 dev-only commits |
| Draft delta | 52 paths, 7,552 additions / 72 deletions |
| Overlap | 13 paths; five conflicted in isolated merge-tree simulation |
| Reusable foundation | workspace context/store and workspaceview have no upstream overlap since the ancestor |
| Stop/pause defect | standing tick/store files are byte-identical between reviewed draft and dev; shared exposure is strongly indicated, but the reproducer was executed only on the draft |

**Recommendation:** use an isolated integration candidate retaining #662 ancestry.
The limited conflict surface makes this a smaller preservation risk than manually
reconstructing a mature patch from 23 mixed commits. Preserve newer dev completion
behavior and carefully reapply the organization seams. Inspect automatic merges too.
Do not interpret a clean merge as proof of behavioral compatibility.

| Conflict | Required reconciliation |
| --- | --- |
| `internal/session/checkpoint.go` | Preserve newer dev tool-result pairing, handoff and continuation behavior; add the organization completion seam. |
| `internal/session/checkpoint_write_evidence_test.go` | Preserve the upstream valid same-message parallel-call fixture. |
| `internal/session/task_run.go` | Keep both ConversationHistory and Organization inheritance. |
| `cmd/aforge/chatv3.go` | Preserve the upstream NoStandingTicks condition and add organization wiring. |
| `PERF.md` | Reconcile both relevant sets of claims without duplicating stale evidence descriptions. |

Audit the other eight overlapping paths even if Git merges them automatically.
Then run affected suites and agreed acceptance on Spark at the exact candidate.
The prior draft's passes do not transfer automatically to the combined revision.

**Fallback:** if contract refinement requires independently reviewable foundation
slices, branch from dev and port the final storage/projection patch followed by the
adapted application seams. Preserve migration tests, IDs and immutable history.
Avoid blindly cherry-picking broad mixed commits or recreating the implementation.
Keep #662 and its evidence until the replacement has been accepted.

## Active iteration constraints

C19–C21 authorize the first build and parallel subagents. No tui3 suite or broad
expensive UI/E2E acceptance in this iteration. Use focused backend functional
checks on Spark, retain exact revision and receipts, and mark broader acceptance
as deferred. Do not mark the full target or all lifecycle work complete after
fixing one defect. The root owns combined integration, checklist and review.

## Draft consolidation

The user requested one maintained draft instead of hanging parallel drafts.
Use #662 as the single active implementation/design record. Consolidate #663's
committed grooming history into the validated candidate, update #662 with the
exact baseline and evidence, then close #663 only after its records are retained.
No successor implementation PR is to be opened for this iteration. Internal lane
branches/worktrees are temporary integration tools, not separate product drafts.

Current baseline integration commit: `1cfdbd28be78e29d28eac6c63d25b1f766af95e3`,
parents backend `c63e03b7` and dev `996117314`. Independent semantic review found
no introduced merge regression; the build and functional checks remain pending.
