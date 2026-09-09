# Five product journeys and evidence boundaries

These are proposed complete journeys. The smaller first-wave cases were sent to
and acknowledged by implementation task `01a0839c-a6c1-7a93-94f2-be0e4c528f64`.
None is marked passed by this document. The implementation owner's functional
test record, `docs/design/workspace-foundation/FUNCTIONAL-TESTS.md`, may be
available only on its in-flight branch until committed; missing evidence must
remain missing.

## Software 1: two bugs share one cause

The person authorizes fixes for two issues. Separate conversations each own a
task numbered 1. An architecture decision applies to both; a third issue merely
shares authentication vocabulary. One investigation finds a common cause.

- Memory/context: current architecture constraints and sourced investigation findings.
- Trigger: a finding gives an actual reason to compare the efforts.
- Coordination: share evidence or a focused investigation without losing either issue.
- First wave: ordinary chat resolves both fully qualified task identities and their real owner state; applicable context reaches authorized workers; grouping/reading does not launch work or permit merging. Unavailable owners are described honestly.
- Full journey: bounded peer consultation, correct origin, independent issue completion, no duplicate shared work under retry/restart, and test evidence for both fixes. Similarity alone does not start a collaboration.

## Software 2: API contract changes across backend, frontend and docs

The person authorizes one feature. One accepted contract applies to three
efforts. A discussion suggests a field rename; a later accepted change confirms
it. Each effort has already seen the old wording.

- Memory/context: one sourced current contract and preserved revisions, distinct from speculative messages.
- Trigger: an accepted revision affects ongoing work.
- Coordination: the three efforts reconcile compatibility and implementation, preserving the agreed goal.
- First wave: create/revise explicit sourced information through real chat; explicitly resume separate consumers; inspect artifacts using the same record ID/current revision; unrelated work receives no automatic context. This does not establish accepted-instruction semantics.
- Full journey: the change causes a justified response at the agreed boundary; actual backend/frontend user flow and docs agree; incompatible intermediate state is not called done; permission to implement does not silently become permission to publish.

## Software 3: ongoing repository maintenance

The person asks aforge to investigate eligible dependency/build problems and
prepare fixes within an upgrade policy. A repeated alert refers to the same
underlying problem. The person later stops the responsibility.

- Memory/context: supported versions, policy, previous outcomes and any retained useful method.
- Trigger: a dependency notification, meaningful build failure or scheduled check.
- Coordination: maintenance consults affected feature work when necessary.
- First wave: resolve an existing standing record and use shared upgrade-policy information in ordinary resumed work. The current implementation owner explicitly says scheduled firing does not yet receive that new context.
- Full journey: real activation uses current applicable policy, avoids duplicate substantive work across repeat/restart, produces inspectable test/PR evidence and obeys stop. A routine no-change check stays quiet. Connector fixtures do not imply real GitHub publication was tested.

## Non-software 1: research corrects a marketing campaign

A sourced competitor finding informs an authorized draft. Later evidence
contradicts it. The consumer transcript still contains the original claim.

- Memory/context: source, uncertainty and revision/withdrawal; research is not permission to change campaign goals.
- Trigger: a meaningful contradiction or accepted correction.
- Coordination: research and campaign work establish what remains supportable.
- First wave: revise/withdraw a shared record, explicitly resume/reopen the same consumer, and inspect a new draft. Old claims must not remain established facts; source/history stay inspectable; no invented replacement or unauthorized publication.
- Full journey: discover affected work, explain the source and consequence, revise authorized drafts or request the uncovered decision. Test an unrelated similar claim as a negative case. Decide separately whether existing published material creates follow-up work.

## Non-software 2: travel information affects a calendar

The person asks for reporting on conference changes affecting an itinerary and
calendar. A synthetic email changes a meeting time. The same event arrives
again and after restart. The watch is eventually stopped.

- Memory/context: confirmed itinerary, relevant preferences and handled-event state, each retaining its distinct meaning.
- Trigger: schedule-change email or a scheduled check.
- Coordination: travel planning consults calendar context to identify actual conflicts.
- First wave: a real consumer uses sourced schedule information to produce an impact artifact. There is no calendar-write connector fixture in the current wave; do not claim connector authority enforcement was exercised merely because no connector exists.
- Full journey: real intake against deterministic synthetic connectors, one meaningful notification per handled change across duplicate/restart, no calendar writes under reporting-only authority, quiet no-change checks, and stop behavior consistent with the agreed in-flight contract.

## Common acceptance rules

Use real model-driven product entry points and production assembly. DeepSeek V4
Flash is the requested model. Seed reproducible synthetic inputs with fresh
values where needed to prove retrieval, not prior knowledge. Read artifacts,
persisted IDs/revisions/sources, operation receipts, action counts and current
owner state. Convincing final prose and store-only tests are insufficient.

For the first wave, parameterize domain examples when they truly exercise the
same mechanism. Do not pay for five duplicate harnesses. Retain separate cases
where behavior differs: workers, standing references, conflicting context,
restart and unavailable owners. A domain-themed file is not proof of domain
execution. Later tests must exercise the actual API, repository or connector
boundary they claim, using controlled fixtures rather than live personal data.

Exercise memory-off, out-of-scope context, stale transcripts, revision,
withdrawal and reopen/restart where promised. Later activation tests require
the real admission path and deterministic event/time control; directly calling
a response function does not prove triggering. Peer tests require actual origin
and durable delivery evidence. Neither idempotent intake nor message dedupe
proves exactly-once external action.

Each receipt names the tested commit, entry point, model, prerequisites,
assertions, artifacts/logs, duration/spend, and limitations. Missing credentials,
missing tests and skips are not passes. Bound test time and spend. Run cheap
contracts on relevant changes; live-model and actual UI checks cover the promises
made by the slice. A backend pass is not evidence of TUI usability.

Completion states are proposed, confirmed, building, awaiting evidence, passed
on a named commit, integrated, or deferred with a specific missing capability.
Do not mark an entire row complete because one smaller variant passed.
