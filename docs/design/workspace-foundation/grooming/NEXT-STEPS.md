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
- [ ] T01b — Choose the baseline route. Recommendation: preserve the draft, integrate
  current dev in an isolated candidate, then validate. No actual integration or
  branch replacement has been performed; preserve C07 until a change is agreed.
- [ ] T02 — Settle backend composition and independent identities (D11/D12).
  First question is pending: typed entities/components versus generic properties.
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
- [ ] T09 — Produce one implementation-ready contract for the first agreed slice:
  exact baseline, source owners, transitions, interfaces, migrations and positive /
  negative end-to-end assertions. Do not wait to design every future adapter.

## Subsequent delivery

- [ ] T10 — Prepare the chosen isolated implementation baseline; preserve the old
  draft and evidence until replacement/integration is explicitly settled.
- [ ] T11 — Fix lifecycle correctness, beginning with the stop/pause overwrite;
  verify concurrency and next-occurrence behavior on Spark.
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
