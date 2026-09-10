# Critical architecture review and draft refactoring assessment

2026-09-10. This review challenges [ARCHITECTURE.md](ARCHITECTURE.md) and the
[23 scenario walkthroughs](ARCHITECTURE-SCENARIOS.md), then traces the implementation
in backend draft [#662](https://github.com/Agent-Field/aforge-v2/pull/662).
It is not a declaration that every desired journey passes.

**Conclusion**

The architecture is coherent as a direction, but is not yet an implementation-ready
specification. The previous statement that all journeys have a route through the
model was too weak as validation: an arrow labelled “admit,” “assemble context” or
“handoff” can conceal the difficult part. The draft is a useful organization and
informational-context foundation. It has not built the durable, governed,
cross-conversation operating system described by the target. Preserve its sound
pieces; refactor the execution seams and state transitions before adding more
feature families.

A small user vocabulary is not evidence of a small or correct backend. Backend
records with independent identity and lifecycles are justified even if the user
never sees their names. Conversely, a diagram box does not justify another store,
service, manager or universal property framework.

**Evidence and scope**

The implementation baseline is clean commit
`c63e03b7fe8ee84b6b94befa5403002593af0ebe`; GitHub confirmed #662 remains draft at
that exact head. The grooming branch is a different, earlier runtime snapshot;
source claims below refer to #662, not whatever code happens to sit beside this
file. Observed remote `dev` is now `9961173140ba24ff99ef91c8e93fb79e326d854b`.
No integration or rebase was performed. All file/line citations below are pinned
to the reviewed backend revision.

The review covers domain boundaries, each sequence's ownership and failure
contracts, all 23 selected journeys, the draft's organization/context code,
execution constructors, standing persistence and previous evidence. It is not an
exhaustive security review, enterprise permission design, connector certification
or model-quality benchmark. The reference-product sources remain linked in the
scenario review; no further feature-parity claim is made here.

**A. Criticism of the proposed architecture**

| Finding | Concrete counterexample | Required correction / recommended decision |
| --- | --- | --- |
| A1 — Context mixes knowledge, interpretation and governing direction | A model writes an “accepted” summary and later workers treat it as authorization | Keep immutable semantic revisions, but make governing adoption an explicit authority-owned transition tied to the original user message and scope. Knowledge can update without changing authority. A boolean or model-written enum is insufficient. |
| A2 — Effective context has no single version | A run records Context revision 3, but its folder placement, exception or profile changes while the text remains revision 3 | Record an effective-input manifest: source revisions, adoption/scope revisions, membership generation where relevant, profile version and authority revision. Revalidate materially affected work at defined boundaries. |
| A3 — Scope defaults remain too open | One chat is in Product and Marketing; both supply conflicting directions | Recommend explicit governing bindings separate from navigation. Placement may propose a governing binding; a shortcut alone never creates one. Conflicts at equal authority are unresolved, not silently settled by folder order or recency. Preserve prior explicit direction until superseded. This refines P1 rather than assuming folder names settle scope. |
| A4 — “Mandatory context survives bounds” is not an algorithm | Eight long applicable requirements cannot fit the six-record snapshot | Resolve the complete applicable obligation set before execution; use a compact index and exact source reads, not oldest-first truncation. If conflicting or too large to establish required preconditions, split the work or suspend the affected action. Never claim arbitrary finite context can contain unlimited obligations. |
| A5 — Work, Run and continuation are not precise enough | A CI callback arrives after pause, specification edit and worker replacement | Define separate specification revision, control revision, attempt identity, wait token and execution fence. A callback can close its historical wait without starting new work. Current owner/control state decides whether continuation is admitted. |
| A6 — Named assistants are squeezed into Chat or Work | A named reviewer owns three duties; one duty is deleted or reassigned | Introduce a supporting execution-identity record when identity spans duties. Keep profile configuration, identity, responsibility and process lifetime separate. Route messages to stable identity plus an explicit work/conversation target. It need not become another user-facing object or permanent process. |
| A7 — Crash recovery can sound like exactly-once external action | A payment/form/email succeeds remotely, but the acknowledgement is lost | Guarantee durable intent and deduplicated admission locally; claim exactly-once effects only where the adapter can enforce it. Otherwise record uncertain outcome and reconcile or request a decision before retrying the effect. A local lease cannot fence an arbitrary remote browser. |
| A8 — Trigger composition still hides a workflow engine | “After both” joins yesterday's release to today's unrelated window | Specify correlation keys, event-time/arrival-time choice, validity window, lateness, expiration, consumption and overlap. Start with schedule/single-event triggers; ship compound activation only with these semantics. Keep it a typed component, not arbitrary executable metadata. |
| A9 — Automatic impact has no measurable promise | A relevant Product change never reaches Marketing, despite no explicit dependency | Promise deterministic propagation for declared dependencies and adopted applicable direction. Treat semantic discovery as best-effort with measured recall, latency and cost. It must not be the sole enforcement path for a known obligation. |
| A10 — Multiple stores make migration and recovery expensive | Context is committed, a JSONL owner is not, and only one store is restored | Keep a single authority per lifecycle and durably publish changes. Prefer a reconstructible intake index before a new coordination store. P2 requires a demonstrated atomic-admission need, not convenience. Define a consistent backup boundary and restore reconciliation; “every migration is atomic” cannot mean a magical transaction across files and databases. |
| A11 — Missing records can be hidden as absence | A damaged responsibility disappears from listing while the user believes it is running | Projection distinguishes missing, inaccessible, incompatible and temporarily unavailable. Execution fails inactive for unknown behavior; inspection still exposes an actionable unavailable record. One bad item must not stop unrelated work. |
| A12 — Extension contracts need behavior, not only schema validation | A new connector preserves its fields but retries an external write unsafely | Require adapter conformance for identity, cancellation, authority, replay, effect uncertainty and delivery receipts. Do not build a general plugin platform or generic property database before real use demonstrates the need. |

These are concrete refinements, not reasons to discard the model. A1–A5 and A7
are prerequisites to calling autonomous operation reliable. A6 matters when
persistent roles spanning duties are included. A8 should be staged instead of
quietly expanding the first implementation into a general workflow engine.

**Recommended transition contract**

The following is proposed engineering behavior; product wording remains open.

| Operation | Owner transaction | Consequence for running/waiting work |
| --- | --- | --- |
| Edit responsibility | Compare expected specification revision, append a new version, publish change | Old results retain old inputs. Admission decides whether to finish, recheck or replace; no blind mutation of the running attempt. |
| Pause | Advance control revision, disable new admissions | Existing activity obeys a declared pause mode; default finish a non-interruptible effect then stop at the next safe boundary. No next recurrence or callback starts work while paused. |
| Stop | Advance control revision and cancel owned outstanding execution | Preserve evidence and uncertain effects. Late results may be recorded but cannot reactivate the responsibility. Stop is not rollback. |
| Accept completion | Compare current outcome/specification and required evidence | Computation, artifact landing and external delivery have separate receipts. A stale successful result cannot silently satisfy a revised outcome. |
| Resume a wait | Consume its correlation token once against current owner state | Resume the same unfinished outcome when still valid; otherwise retain disposition without starting an unrelated fresh occurrence. |
| Change profile or governing direction | Publish a new configuration/authority revision | Narrowed authority is checked before the next effect. Previously completed effects remain historical facts. |

The implementation need not rename existing task states. These are invariants for
adapters over current owners, not a replacement state machine for every package.

**B. What the draft really does**

| Area | Source evidence at #662 | Assessment and action |
| --- | --- | --- |
| Organization ownership | [workspace store](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/workspace/store.go#L99) stores collections/context; resolver reads live owners | **Keep.** No copied task status or physical session migration is required. |
| Logical references | [Ref](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/workspace/workspace.go#L34) qualifies task numbers with session identity; artifacts use absolute paths | **Extend.** Preserve IDs; add source message/event anchors and artifact version evidence. Paths alone cannot support historical truth or external resource identity. |
| Context versioning | [ContextRecord and revisions](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/workspace/context.go#L24) retain source Ref, target set, immutable revisions and expected-revision edits | **Keep storage discipline; extend semantics.** No adoption, instruction authority, exceptions or effective scope version exists here. Do not bulk convert informational records into instructions. |
| Scope | [contextScope](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/workspace/context.go#L355) matches explicit targets and direct membership | **Refactor once policy is settled.** Ancestor exclusion is deliberate and tested, not a broken implementation of recursive inheritance. Task scope also includes its originating conversation. |
| Prompt snapshot | [OrganizationContext](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/session/organization.go#L56) injects up to six records, 1,200 runes each, explicitly informational | **Keep as optional evidence presentation; do not reuse as the obligation enforcement algorithm.** Complete reads and paging exist but model choice to retrieve is not guaranteed governance. |
| Chat refresh | [turn seam](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/session/loop.go#L335) refreshes before model activity | **Extend.** No durable notification from a context edit to an idle or active unrelated consumer is established by this call. |
| Ordinary workers | [newTaskAgentOn](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/session/task_run.go#L6575) inherits Organization | **Keep; generalize the application contract.** Qualified task/root identities provide a useful seam. |
| Completion versus separate checker | [completion page](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/session/checkpoint.go#L3135) appends the turn snapshot; [separate checker constructor](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/session/task_audit.go#L2726) does not inherit Organization | **Refactor.** These are different execution paths. Passing one completion-reader test does not prove current obligations reach independent checking. |
| Forked specialists and adaptive nodes | [fork constructor](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/session/fork.go#L926), [adaptive constructor](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/session/orchestrate.go#L1062) construct separate configurations | **Audit supported doors.** Copied prompt text is not a fresh scope/authority contract. Planned adaptive runs are no longer a normal chat route; do not rebuild dead entry points just to make a matrix look complete. |
| Worker-produced shared learning | [organization tools](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/session/tools_organization.go#L180) refuse worker mutation | **Preserve authority separation; add a result-publication path.** Autonomous Product-to-Marketing findings cannot rely on a worker directly invoking the current write tool. Publish sourced findings to the owner; adoption remains separate. |
| Scheduled work context | [ticker posture](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/cmd/aforge/chatv3_standing.go#L177) omits Organization; [standingRunConfig](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/session/standing_run.go#L766) copies that posture and uses a run Place | **Refactor both configuration and identity resolution.** Merely setting Organization is insufficient: scope must resolve the standing owner, not just the new run's conversation ID. |
| Timers and retained runs | [Ticker](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/standing/tick.go#L43), existing When/Action/Rails components and numbered run directories | **Keep and evolve.** Already real machinery; do not introduce a competing scheduler. Single-ticker locking is not event admission or transactional effect recovery. |
| Control persistence | [Save](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/standing/store.go#L84), [firing writeback](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/standing/tick.go#L462), [stop/pause](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/session/tools_standing.go#L1396) write whole Item documents | **Refactor.** Per-write file locks prevent torn files, not stale writes. Revision-aware owner operations must preserve concurrent control changes. See reproducer below. |
| Crash after external effect | [fire](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/standing/tick.go#L384) calls Runner before recording ledger/item completion | **Missing recovery contract.** A successful effect followed by process death is not reconciled merely by another tick. This is a structural risk, not a reproduced external duplicate in this audit. |
| Folder editing from chat | [collections schema](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/internal/session/tools_organization.go#L20) exposes list/show/find/create/add/remove | **Extend application operations.** Store Rename exists, but is not exposed through this tool. “Chat is the main control surface” is not satisfied by a lower-level store method alone. |

The present draft intentionally excludes several target capabilities. That does
not make the foundation wasted or deceptive. The mistake would be calling its
successful storage tests proof of autonomous journeys, or layering more prompts
on top of missing lifecycle contracts.

**C. Journey validation status**

All 23 journeys were reconsidered against counterexamples. Their actual completion
status is mixed; “conceptually representable” is not a passing test result.

| Journeys | Existing foundation | Blocking target contract |
| --- | --- | --- |
| J01 direct chat; J12 traceability | Saved conversation, live owner resolution, optional search/context | Exact source/version retention and final scope contract; no new backend for ordinary direct chat. |
| J02 Slack; J07 personal assistance; J13 restart | Standing timer/run/inbox machinery | Account adapters, scoped scheduled context, reliable admission/delivery and restart policy. |
| J03 shared investigation; J04 shared API; J06 research impact; J11 discovery | Context records and ordinary task work | Owner-published findings, dependencies, addressed consultation, duplicate claims and impact delivery. |
| J05 factory/maintenance; J08 production checks; J14 delayed CI/stop | Task execution/checking and existing control paths | Maintenance scope, evidence-specific completion, callback tokens, cancellation/writeback and uncertain-effect recovery. |
| J09 folder moves/exceptions | Multiple memberships and direct-context selection | Governing binding, conflict resolution and effective scope version. |
| J10 learned method; J21 reuse | Files and optional memory | Portable versioned profile/method contract, correction and no private-state copying. |
| J15 specialists; J16 nested work | Existing child work and limited fork tools | Consistent context/authority contract, descendant budgets, owner reassignment and stop propagation. |
| J17 named role; J18 shared discussion; J22 organization | Names, tasks, conversations | Stable execution identity when spanning duties; origin-aware routing and ownership transfer. |
| J19 accounts; J20 computer use | Host/tool foundations where available | Inventory and demonstrate account-bound adapters, shared resource ownership and credential expiry recovery. |
| J23 extension/compound triggers | Typed single When variants | Join semantics and extension conformance; do not claim arbitrary composition today. |

**D. Refactoring order and acceptance gates**

1. **Establish the candidate.** Integrate relevant current dev into an isolated
   implementation branch, reviewing the existing evidence, history, questions and
   stop changes cited in the architecture. Reconcile overlapping completion changes;
   do not independently reimplement them. Record the exact integrated revision.
2. **Make owner transitions safe.** Fix stale standing writeback, introduce versioned
   control/admission operations and preserve existing grants, scopes, IDs, counters
   and run history. Acceptance starts with pause/stop during an in-flight execution,
   duplicate occurrence, and crash between effect and completion receipt.
3. **Make current direction real.** Define authority-owned adoption, source anchors,
   governing applicability and the effective-input manifest. Adapt chat, ordinary
   workers, separate checking and scheduled execution to one contract. Test an
   accepted requirement and its correction with memory disabled, across reopen,
   delegation and a scheduled run; inject enough optional context to exceed limits.
4. **Close one recurring journey end to end.** Daily Slack with a deterministic
   connector fixture, scheduled delivery intent, correct run identity, an unanswered
   question, repeated event and restart. Measure actual action counts and receipts.
5. **Close one cross-work journey.** Product finding invalidates Marketing output;
   worker publishes evidence, owner processes it, appropriate consumer receives it
   without manually reopening, and duplicate work is suppressed. Contrast an
   informational finding, accepted direction and unauthorized new obligation.
6. **Add persistent specialist/routing boundaries as needed.** Reuse child execution;
   add explicit identities/profiles and account/resource bindings. Validate a handoff,
   conflicting peer message, narrow tools and stopped descendant.
7. **Expand triggers and discovery after the contracts work.** Compound conditions,
   reusable templates and semantic association extend proven seams. Evaluate recall
   and cost; avoid a second generic scheduler, graph engine or memory store.

Each stage should retain existing behavior unless deliberately changed and documented.
A passing lower stage does not pass the higher ones. Keep #662 draft until the
chosen slice has fresh evidence; the full target is not the merge criterion for
an explicitly scoped foundation PR, but its title/manual must accurately bound it.

**E. Validation receipts**

The original #662 CI checks are green, but its paid DeepSeek functional job is
skipped. The prior independent behavioral audit retained false success reporting,
missing landing disclosure, watch-setup recovery and suggestion-as-acceptance
failures; see the pinned [handoff](https://github.com/Agent-Field/aforge-v2/blob/c63e03b7fe8ee84b6b94befa5403002593af0ebe/docs/design/workspace-foundation/HANDOFF.md#independent-behavioral-audit--2026-09-09).
Those are historical evidence, not fresh failures reproduced here or repaired now.

Fresh Spark receipts follow. No full suite runs on
the laptop, and deterministic package tests do not prove real connector/model journeys.

| Fresh check | Exact candidate | Spark receipt | Result and limit |
| --- | --- | --- | --- |
| Existing complete package suites: workspace, workspaceview, session, standing | Unmodified backend `c63e03b7fe8ee84b6b94befa5403002593af0ebe` | `20260910-150118-000406`, 15:01:19–15:04:42 UTC | PASS, exit 0. Session 193.162s; workspace 5.504s; workspaceview 0.289s; standing 0.119s. Repository target applies its existing known-red exclusion. No paid model/connector E2E was run. |
| Deterministic pause/stop during firing | Same backend plus only the portable audit test below | `20260910-150249-000407`, 15:04:42–15:04:43 UTC | FAIL, exit 1. Both paused and retired were overwritten to active. This demonstrates a control-state persistence defect, not a model or UI test. |

The suite command was
`make test PKGS="./internal/workspace ./internal/workspaceview ./internal/session ./internal/standing" TEST_FLAGS="-count=1 -timeout=15m"`.
The rsync test checkout produced Git build-metadata warnings because its worktree
pointer refers to the laptop. The logged test invocation and all four package
results completed; this was not a build/merge-acceptance claim. No runtime files
were modified for that suite.

**Confirmed defect R1 — a firing can undo a concurrent pause or stop (high priority).**
The ticker reads an active Item, calls the runner, then saves that old whole Item.
During the call, another operation can successfully save paused/retired state.
The ticker's final Save restores active state and can erase the retirement reason.
The item lock serializes individual writes; it does not protect the read–execute–write
interval. A single ticker lock does not serialize user edits. The reproduction
uses a callback to impose this exact ordering, so it requires no sleeps or chance.
It reaches the same store write used by the current stop/pause operation, but does
not drive the user interface or a real external effect.

Refactor the owner so completion updates runtime fields against a recorded
specification/control revision and preserves newer control state. Do not hold
an item lock across a long model/network call. Revalidate control before effects,
fence obsolete attempts where enforceable, and test pause/stop, grant narrowing,
exception changes and configuration edits during both quiet checks and firings.
A fix must also prove that the next occurrence stays inactive after stop; preserving
a status string alone is not end-to-end acceptance. R1 was reproduced on the draft;
this audit has not established whether current dev has the same failure.

**Portable reproduction**

The fixture is [standing-control-repro.go.txt](validation/standing-control-repro.go.txt).
SHA-256: `222e85d24bd1d85e868ba06fb8e5e97651d6f4dc732893f96e89d1a04294613a`.
It is retained as documentation, not installed into the default suite as a known
failing test. It uses that revision's existing deterministic test helpers.

To reproduce, create an isolated checkout of the pinned backend revision, copy
this fixture to `internal/standing/architecture_audit_test.go`, and submit from
that checkout's repository root through fleet on Spark:

```sh
fleet run --cpu --json 'go test ./internal/standing -run "^TestArchitectureAuditControlSurvivesFiring$" -count=1 -v'
```

Expected failure on the reviewed revision:

```text
control was overwritten: saved paused during firing, got active
control was overwritten: saved retired during firing, got active
```

[Suite output](validation/foundation-suite.log) and
[reproducer output](validation/control-repro.log) retain the job receipts.
No runtime fix, schema migration, upstream integration or merge was performed.
The next implementation slice should address R1 and the authority/execution
contracts; adding more organization metadata cannot resolve them.
