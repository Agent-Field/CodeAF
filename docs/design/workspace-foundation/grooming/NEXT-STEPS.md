# Working checklist

## Current — integrated baseline and the W5-B slice (2026-09-14)

**Everything below this section is history.** Read its statuses as what was true
when each was written; the current source and this section are authoritative.

- **Integrated baseline:** `codex/personal-ai-backend` at `3788e569e`, the W5-A merge
  (chat edits of ongoing work, one live owner per report path, the brace refusal),
  on top of W5-B watch conditions, the folder view, the prefix budget and round 4b.
  Draft #662 against `dev` stays the one record; nothing merged, released or deployed.
- **This slice, W5-B journey:** lane `codex/personal-next-0914`, final runtime
  `a3252e8f3`. Record: [BUILD-WAVE-05B.md](BUILD-WAVE-05B.md). Quarantine:
  `/home/santosh/af-pai-integrate` is not part of any integration.
  - [x] Chat-driven local-file journey with a real model, one conversation: create, edit
    instructions, edit watch, edit report path, pause, resume, stop, placement governs and a
    reference does not. Live 1 29/33 at `454617ad5`, live 2 31/33 at `e3201af9c`
    ($0.0869); the remaining live-2 failures are one run-variance miss and one driver
    false failure, both recorded.
  - [x] A file watch's `when ·` line is said by its pattern on every surface (G1; regression
    fails at `3788e569e`).
  - [x] A report path changed by an edit, not a stop and a new card (G2; schema regression
    fails at `3788e569e`; one live sample).
  - [x] Two folders into one report: brace refused, one live owner, no card claiming an
    unwatched folder, no silent broadening (live 1 and 2; manual section).
  - [ ] Whether the chat's stop should be confirmed on a card — open decision for the owner.
  - [ ] The driver answers questions as a person (today the session's default dial does).
  - [ ] Model-written `when_words` for moments, rhythms, idle waits and probes are still shown as sent.
  - [ ] Full tui3/session/cmd suites and the tagged E2E package were not run in this slice.

Updated 2026-09-10. This is the single execution checklist for the current review
and implementation preparation. [DECISIONS.md](DECISIONS.md) remains the authority
for accepted behavior; [CRITICAL-REVIEW.md](CRITICAL-REVIEW.md) contains the evidence.
A checked item means completed with the evidence named here, not merely discussed.

**Active continuation checkpoint (2026-09-10):**
[CONTINUE-WAVE-03.md](CONTINUE-WAVE-03.md) records the running Claude Code Opus
job on Spark, queued independent review, exact worktree/branch, failed and passing
local-file E2E evidence, and the remaining integration steps. Read it before
starting another agent or interpreting this checklist as current runtime status.
Wave 03 is still in progress; its final live journey and integration are not checked off.

**Superseded the same day (about 22:40 UTC):** wave 03 finished and is integrated
into this branch. Final source `c0de4d8f9`, focused validation 11/11 at `1c7012818`,
live6 `DEMO PASSED` ($0.0114); the lane `b48387207` was merged with no runtime
difference. The wave-03 execution checklist and its evidence are in
[CONTINUE-WAVE-03.md](CONTINUE-WAVE-03.md) (completion section and "Remaining
execution checklist"); the delivery record is "Local-files ongoing-work wave" below.

## Completed

- [x] Consolidate architecture, persistence and seven operating sequences.
- [x] Walk through 23 selected user journeys and counterexamples.
- [x] Critically review the architecture and trace backend draft #662 at
  `c63e03b7fe8ee84b6b94befa5403002593af0ebe`.
- [x] Run existing workspace/workspaceview/session/standing package suites on Spark:
  job `20260910-150118-000406` passed.
- [x] Reproduce concurrent stop/pause overwrite on Spark:
  job `20260910-150249-000407` failed as documented. **The fix now passes the focused combined validation below.**

## Current: settle decisions and choose the baseline

- [x] T01a — Complete two independent baseline reviews, including an isolated
  merge-tree simulation. Findings and differing recommendations are recorded below.
- [x] T01b — Baseline route accepted: preserve the draft and integrate dev in an
  isolated candidate. Integrated and validated; existing #662 remains the single implementation draft.
- [x] T02a / D11 — Typed backend entities and composable components accepted (C19).
- [ ] T02b / D12 — Independent persistent identities remain to settle.
- [x] T03a — Recommended governing scope/conflict behavior accepted (C23).
- [ ] T03b — Complete source-backed retention and authority implementation (D01/D02).
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

- [x] T10 — Isolated integration baseline prepared and statically reviewed;
  draft ancestry and newer dev behavior retained. Parent #662 is preserved.
  Runtime validation is tracked separately below.
- [x] T11a — Candidate implementation and manual completed; owner operations,
  stale-write detection, runtime delta merge and schedule reconciliation reviewed.
  Source commit `5186fda3e`, integrated as `3f57455ca`; confirmed by the focused Spark run below.
- [x] T11b1 — Independent regression suite written; baseline Spark job
  `20260910-153136-000409` fails all five top-level tests (13 scenarios), including
  unwanted subsequent execution. Test commit `8dedb0bb6`; runtime 0.020s.
- [x] T11b2 — Combined candidate `55bfc0118` passed Spark job
  `20260910-154640-000411`: small backend packages, selected session cases and
  `make build`. Independent tests use the production status API.
- [ ] T11c — Broader admission/cancellation/recovery contracts remain future work.
- [x] T12a — Explicit governing placements and folder-scoped holds, separate from references.
- [x] T12b — Read-only governing context across chat, workers, independent checking and scheduled runs.
- [x] T12c — Consumed-context records for journaled turns and bounded in-aforge inspection (C24); gaps remain below.
- [ ] T12d — Exact automatic source-backed retention, clause-specific exceptions, action-boundary re-admission, ephemeral fork receipts and complete causal handoffs.
  Partial (wave 03): standing task runs record their occurrence as `parent_cause`;
  tasks, forks and other executions still record `not_recorded`.
- [x] T13 — Re-scoped by the owner on 2026-09-10 to **local files only** (no Slack,
  real or simulated; no connector). The ongoing inbox-report journey passed:
  setup, delivery (report plus project inbox note), edits, a report held for the
  person, pause/stop, and restart after a kill. Evidence is in BUILD-WAVE-03.md
  and the "Local-files ongoing-work wave" section below. The Slack version is
  withdrawn, not done.
- [ ] T14 — Complete cross-work Product-to-Marketing impact, including worker
  publication, duplicate suppression, authority and no-manual-reopen activation.
  Partial (wave 03): a local spec change wakes a Marketing review through an
  explicit file watch. The review's report is owner-published and checked against
  the Marketing rule, it is not rerun on an idle pass, and a reference does not
  govern. Impact between works without an explicit watch is not built.
- [ ] T15 — Extend proven seams to persistent roles, handoffs, reusable profiles,
  additional adapters and compound triggers; tick each supported journey only
  against its own real acceptance evidence.

For each delivery item, record the exact candidate revision, Spark job, result,
remaining failures and integration destination. Passing package tests is not
passing model/connector acceptance. Existing draft/manual/release restrictions
remain in force; no merge into dev, release or live deployment has been performed here.

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

## Baseline review — completed, recommended route accepted

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
no introduced merge regression; the subsequent build and functional checks passed.

The [independent old-behavior receipt](validation/lifecycle-baseline.log) is
retained separately from the earlier two-case reproducer. It is failing baseline
evidence, not a test result for the implementation candidate.

## Completed first functional slice

- Integrated through the temporary `codex/personal-ai-integration` candidate;
  no additional PR was opened. The maintained destination is #662.
- Exact code/test revision submitted: `55bfc0118ff9371b9abd3a2524e8dbb3930bb04d`.
- Independent tests: original `8dedb0bb6`, follow-up `2234b58b2`; final fixture
  covers 17 scenarios, alongside seven narrow owner tests.
- Spark job: `20260910-154640-000411`. Command runs small standing/workspace/view
  package suites, selected session control/context/history/evidence cases, then
  `make build`. No tui3 test or broad UI/E2E acceptance is included.
- Result: **PASS**, exit 0, 2026-09-10 15:46:42–15:47:11 UTC (29 seconds).
  Standing 0.152s, workspace 5.547s, workspaceview 0.202s, selected session 1.221s.
  The remaining time includes compilation and the normal packed-manual build.
- [Retained full receipt](validation/lifecycle-functional-pass.log). Build revision
  is `55bfc0118`. Later bookkeeping changes affect documentation only; runtime
  and test source remain identical to that revision.
- Transported worktree metadata emitted Git-path warnings; the logged commands,
  package results and binary build all completed successfully. This is not broad
  merge acceptance or a live-model/connector E2E claim.
- Deployment boundary: standing schema 2 reads legacy schema 1 and upgrades on
  write. Old engines/tickers must be stopped and restarted together; no live
  installation or user-state migration has been performed by this iteration.

The main draft's description records the integrated baseline, passing result and
remaining target work. Grooming history is retained before closing superseded #663.
Broad CI remains deferred under C21; candidate publication is not merge approval.

## Consolidation completed

- [x] Validated code and all grooming history retained on
  `codex/personal-ai-backend`, existing draft #662.
- [x] #663 marked merged by GitHub when its history reached #662's branch;
  its description redirects future work to #662.
- [x] #662 description updated with baseline, implemented behavior, exact Spark
  evidence, remaining work and explicitly deferred broad acceptance.
- [x] No additional implementation PR opened and nothing merged into dev.

Next unchecked delivery is T12, preceded by the remaining authority/scope
contract decisions in T03. This completed slice establishes safe owner updates;
it does not complete the entire personal AI architecture.

## Current governing-context wave

Implementation in isolated `codex/governing-context`; destination remains #662.
C23 scope behavior and C24 runtime evidence are accepted. Source lanes are
parallel; a source commit alone does not tick delivery. Spark evidence will be
recorded after combined integration. All local builds/tests are prohibited (C25).

## Governing-context functional slice — completed

- Exact tested source/manual/test revision: `792a9dffc1a372ebcf8a9e217afbf6a62cfdbe66`.
- Spark job `20260910-170155-000415`, **PASS**, exit 0, 17:01:56–17:02:21 UTC
  (25 seconds). Standing 0.177s, workspace 6.403s, workspaceview 0.301s;
  selected session cases including scope, actual child constructors, trace,
  organization and manual tool registration passed in 1.608s. `make build` passed.
- [Exact receipt](validation/governing-functional-pass.log). Initial combined
  source had an early-return compilation error (job000413), fixed before the
  passing job000414; the final run also includes the unfiled identity correction.
- Independent review caught and repaired background placement widening, nil-reader
  interface panic, old compaction mislabeling, oversized trace rows and stale
  call/result links. Retained absent provenance stays absent; no invented causal
  relationship fills a missing execution edge.
- No local build or test ran in this wave. Stale local test-log pollers were
  stopped; remote-log followers were closed after receipt capture.
- No tui3 suite, broad UI/E2E, paid model or real Slack acceptance was run.
  No live installation, user-state migration or merge into dev is claimed.
- Next: T12d authority/source and cause propagation, then T13 complete daily Slack
  journey and T14 cross-work impact. A source selection is not proof that the
  model complied; journal windows are not a complete cause/effect graph.

## Local-files ongoing-work wave — completed

- Lane `codex/personal-e2e-opus`, base `72660bf57`, destination #662 through
  `codex/personal-ai-backend`. Final source revision `c0de4d8f9`, after final
  review 425's two publish-path blockers; plan and receipts are in
  [BUILD-WAVE-03.md](BUILD-WAVE-03.md).
- Focused Spark validation at `1c7012818` (source `c0de4d8f9`): **PASS, STATUS 0**,
  11 of 11 steps, 0 skips, 22:20:35–22:21:54 UTC
  ([receipt](validation/wave03-validate-final.log)). The earlier pass at
  `7c3312539` is kept as [wave03-validate.log](validation/wave03-validate.log). Deterministic binary journey `TestLocalWorkJourney`
  ran with a scripted loopback model; that is not live-model acceptance.
- Live, on `deepseek/deepseek-v4-flash`, one call at a time, well under $0.10 in total:
  - live6 at `1c7012818` (source `c0de4d8f9`): `DEMO PASSED`, $0.0114, including a
    live refused self-write that published the closed report
    ([log](validation/wave03-live6.log)).
  - live5 at `9cc7b638c`: `DEMO PASSED` ([log](validation/wave03-live5.log)); review 425
    then found the publish blockers at that revision.
  - live4 at `d73ce259a`: the report correction was observed live, then the demo
    failed on a refused self-write ([log](validation/wave03-live4.log)), which was fixed.
  - live2 at `b427dde07`: the privacy violation ([log](validation/wave03-live2.log));
    not acceptance.
  - Real-model rules check on that violation: PASS ([log](validation/wave03-rules-check-live.log)).
- An independent review found blockers 1–2 and issues 3–5, and the symlinked-parent
  `MkdirAll` problem. All are fixed, each with a regression test; the blocker-1,
  publish-crash and own-report-write tests fail on the old logic. Final review 425
  found two more in the publish path: a self-write apology was published, and a
  limit or an unclosed report still published. Both are fixed by one typed
  publish decision; four regressions fail at `9cc7b638c`
  ([receipt](validation/wave03-publish-old-logic.log)).
- Boundaries kept open:
  - The rule check is a model's reading of the published report, not proof and
    not general enforcement.
  - A failed run is not retried by itself.
  - Stop mid-run withholds only aforge's report and note; started effects are
    not cancelled.
  - Terminal-made items do not install the timer.
- Not run: tui3, broad UI/E2E, full session/cmd suites, the chat-card setup path,
  Slack or any connector. No merge into dev, no deployment, no change to the
  person's live state.
- Integrated into `codex/personal-ai-backend` (#662) by a `--no-ff` merge of lane
  head `b48387207`; `git diff b48387207 HEAD -- ':!docs' ':!*.md'` is empty, so the
  validated runtime tree is the draft's.
