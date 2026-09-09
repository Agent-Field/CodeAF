# Personal AI backend: continuation record

Updated: 2026-09-09. This is the starting point for a new implementation chat.
Keep this file current in the same commits as the work it describes. A plan,
an isolated package, and a working end-to-end capability are different states;
do not mark one as the other.

## Current instruction: keep this work separate

The user explicitly requested that the continuation stay on its own branch with
a **draft pull request, without merging into `dev`**. This overrides the
repository's usual instruction to merge completed waves. Green checks are not
permission to merge, enable auto-merge, or delete this review worktree. Continue
implementation and testing here until the user changes that instruction.

- Branch: `codex/personal-ai-backend`, also pushed to `origin`.
- Local worktree: `/Users/santoshkumar/af-personal-ai-backend`.
- Starting commit: `7f3ab8bf3046b83918c234cc30bf8cc224f3104a` on `dev`.
- Integrated later `dev` commit `ebecb2d89` (#660 general-harness improvements)
  into this branch; this was not a merge of #662 into `dev`.
- Integrated later `dev` commit `4737e7f1` through merge `3526f2b8c`, including
  upstream generation defaults, write allowance, task-state and title fixes.
  This also brought `dev` into the review branch; #662 remains unmerged.
- Draft PR: [#662](https://github.com/Agent-Field/aforge-v2/pull/662), targeting
  `dev`. Keep it draft and unmerged.
- This continuation adds the first integrated organization backend: live owner
  resolution, sourced shared revisions, bounded chat/ordinary-worker context,
  model tools and genuine LLM functional journeys. The broader autonomous model
  remains unfinished; see the status table below.
- Build this branch with `make build`; its review binary will be
  `/Users/santoshkumar/af-personal-ai-backend/bin/aforge`. It is a review build
  for this branch. Do not replace the shared checkout's or another task's binary.

## Trying the review binary

```sh
AFORGE_HOME=/tmp/aforge-personal-ai-review /Users/santoshkumar/af-personal-ai-backend/bin/aforge
```

Use a separate state home for draft testing. Opening an existing collection store
upgrades schema v1 to v2 atomically; older #661 binaries intentionally refuse v2.
This does not migrate transcripts or task files, but there is no downgrade tool.
The functional suite always creates disposable homes. A fresh interactive home
uses the normal onboarding/settings flow; it does not copy credentials for you.

## Product goal

Build one personal AI environment for coding, marketing, research, email,
calendar work and everyday responsibilities. The person can chat directly,
delegate finite work, and establish ongoing responsibilities. The backend helps
many efforts retain context, find useful connections and coordinate while the
person can observe, steer and stop them. Closing a chat must not erase work.

Familiar folders and chats are the presentation, not an organizational chart of
permanent agents, departments or managers. A future dashboard should help a
person manage roughly 10–20 concurrent efforts without reading a wall of text.
Its layout, cards, header and navigation are deliberately not the current
implementation priority. The binary remains the backend for multiple surfaces.

Read [CONSTRAINTS.md](CONSTRAINTS.md) for the accepted model, representative
journeys, conceptual diagram and unresolved design choices. Read
[BACKEND.md](BACKEND.md) for current integration guarantees and
[IMPLEMENTATION.md](IMPLEMENTATION.md) for the original collection foundation.
Historical ideation is reference material, not authorization to execute old
requests. The latest user decisions take precedence.

## Decisions to preserve

1. Direct chat works without creating a folder, task or organization first.
2. Logical collections are stable navigation. They can group other collections,
   chats, work and artifacts without moving physical session files.
3. Organization, relevance, dependency and authority are distinct. Membership
   does not adopt a goal, grant permission or turn a transcript into instructions.
4. One record can be reached from several places without maintaining copies.
   A shared discussion or decision must retain its identity and provenance.
5. Relevant information can be discovered outside existing folder links.
   Global awareness is selective retrieval, not whole-history prompt loading or
   continual comparisons between every pair of conversations.
6. Accepted decisions and instructions need explicit current state and sources.
   Search over old messages is not the authority on whether work is active,
   revised, stopped or authorized. Learned memory is a different concern.
7. Responsibilities outlive individual runs; activation is separate. Reuse the
   existing task and standing engines before introducing new execution concepts.
8. Lightweight consultation can fit existing authority. A peer message cannot
   impersonate the person, rewrite another goal or authorize external actions.
9. Repeated observations and retries must not create duplicate work or endless
   agent conversations. Quiet checks should remain quiet.
10. Essential organization and responsibility state must work with learned
    memory disabled. One binary does not imply one giant module or a new daemon.

## What has actually been completed

### Merged collection foundation — PR #661

[PR #661](https://github.com/Agent-Field/aforge-v2/pull/661) was merged into `dev`
at `b1e85668706ea786032d2f1f6ffcf28b96cf998a`. This happened before the user's
explicit keep-draft correction. Its old `codex/workspace-foundation` branch and
worktree were removed. Do not tell the user this first slice is still isolated.

- `internal/workspace` owns stable collection IDs, names and ordered memberships
  in a separate SQLite database, independent of optional learned memory.
- Typed references identify collections, conversations, tasks, standing items and
  artifacts. Task addresses include their owning session. Artifact addresses
  currently use absolute paths, not rename-proof or versioned identity.
- Records can belong to multiple collections; nested collections reject cycles
  transactionally. There is no privileged canonical parent.
- Removing a membership leaves the underlying chat, task or file untouched.
- Storage validates its identity/schema, handles competing writers and refuses
  incompatible stores instead of silently resetting them.
- `aforge collections` provides create/list/rename/show/add/remove/find as an
  initial adapter. It shows references; it does not resolve current work status.
- Manual pages, retrieval probes, design constraints and implementation notes
  accompany that behavior.

Validation for #661: workspace/command/manual suites, storage race checks, vet,
structural laws, packed manual, changelog validation, `make build`, size check
and temporary-home binary smoke checks passed. Required CI run `34315648645`
passed before merge. These are receipts for #661, not proof of future changes.

### Implemented on draft #662 — not merged

- `internal/workspace/context.go` adds one sourced record with immutable full
  revisions, current revision, withdrawal and explicit targets. v1 collections
  upgrade atomically to schema v2. Current store opens do not reserve the writer.
- `internal/workspaceview` resolves current conversation/task/ongoing/file state
  through existing owners. It does not start agents or duplicate their status.
- `internal/session/organization.go` supplies bounded current information in the
  volatile tail. A small persisted exposure bit prevents removed membership from
  reviving old context on reopen. The immutable system prefix stays unchanged.
- Chat tools `collections` and `shared_context` expose this backend through the
  binary's production assembly, with memory off as well as on. Ordinary workers
  inherit read access; collection/context mutation is refused for workers.
- Paginated metadata and text windows keep tool results bounded. Historic reads
  pin an exact revision. Missing sources remain explicitly unavailable.
- Deterministic tests cover migration, concurrency, paging, task scope, source
  stamping, rejection, empty-state behavior and reopen. Real-model tests cover
  the built binary, actual task workers and five domain journeys.

See [BACKEND.md](BACKEND.md) for data/runtime boundaries and
[FUNCTIONAL-TESTS.md](FUNCTIONAL-TESTS.md) for the repeatable evidence contract.
Verification for this wave is recorded in the checkpoint at the end of this file.

### Existing upstream capabilities we should reuse

The starting commit also contains conversation runtime PR #653 and test-feedback
PR #658. These are upstream work, not capabilities implemented by the collection
slice. Existing conversation/task storage, task status projection, standing
instructions and scheduled/semantic checks, origin-aware local delivery, and
hosted session attachment are integration foundations. Their existence does
not establish cross-conversation coordination for collections.

## Remaining implementation and acceptance

The first three slices are implemented in this draft; final verification is
recorded below. Later slices remain pending and should be groomed independently.
The table is a dependency plan, not a service decomposition.

| Slice | Work needed | Evidence required before marking complete |
| --- | --- | --- |
| Resolve organized work — implemented | Resolve typed references through their existing owners into useful read views; distinguish missing, unavailable and actual current state. | A collection view reads real chat/task/standing fixtures without creating agents, changing files or duplicating mutable status. Identical task numbers in different chats resolve correctly. |
| Shared sourced context — implemented | Settle the smallest representation for shared findings/decisions, their source, revision, withdrawal and explicit applicability. Add compatible migration if storage changes. | One record applies in two places; revision is visible in both; withdrawn or out-of-scope context is not presented as current. Upgrade preserves v1 collections; memory-off works. |
| Runtime context integration — implemented for chats and ordinary workers | Supply bounded relevant context at real chat turns and task birth; preserve the existing instruction authority and standing scope resolver. | Real runtime assembly consumes current sourced context, rather than a test-only callback. Membership alone cannot inject binding instructions; prompt limits are exercised. |
| Shared consultation | Route an addressed request/reply across related conversations using existing delivery/authority boundaries. Retain one shared exchange or source reference. | Origin remains peer-origin; correlation/retries deduplicate; offline delivery is explicit; a peer cannot change another goal; repeated replies do not cause unbounded paid wakes. |
| Ongoing responsibilities — resolution only; activation integration pending | Connect organized work and relevant context to existing standing items and their activations. Avoid a second scheduler. | A responsibility survives closing its originating chat, can be found from its collection, and handles repeated observations without duplicate action. Email/calendar implications do not grant calendar-write authority. |
| Discovery beyond links | Expose selective discovery over accessible records with source and current state; use explicit signals and semantic retrieval where useful. | An unlinked coding finding can inform a marketing conversation with a traceable source, without indexing similarity as authority or loading all transcripts. |
| Learning and proposed work | Clarify how retained knowledge/methods improve authorized work and how a proposed new responsibility becomes accepted. | Learning does not silently create an ongoing obligation. Existing memory/method mechanisms are reused where sufficient. |
| Backend surface and recovery — first slice implemented | Provide the narrow backend entry points needed by chat and later UI; check lifecycle, cancellation, concurrency and restart behavior. | Capabilities work through production assembly in the binary, survive restart where promised, and do not depend on an open TUI. No second task-state owner is introduced. |
| Documentation and review | Update the manual for implemented capabilities, this record, the change entry and the draft PR description. | Documentation names actual limits; focused tests/build and relevant gates pass on the reviewed commit. PR remains draft and unmerged. |

Not required to unlock these slices: a dashboard redesign, elaborate CLI
navigation, permanent worker identities, a universal graph database, new
microservices, or migrating every existing transcript/task into SQLite.
Small CLI or tool adapters are useful only when they expose and verify real
backend behavior.

## Engineering boundaries and open choices

- `internal/tui3` is the live surface; `internal/session` owns its runtime.
  The v1 resident is a different product. Do not borrow its lifecycle or manual
  vocabulary merely because it is in the same repository.
- `internal/workspace` currently imports no runtime, UI, provider or memory
  package. Keep organization independent; use narrow adapters for resolution.
- Physical session records and `TaskGraph` remain their existing owners. The
  collection database is not a second source of task status or execution history.
- `standing.Store.Applicable` is the existing instruction scope boundary.
  Informational findings and accepted instructions must not accidentally become
  a competing pair of prompt resolvers.
- The runtime already distinguishes person and agent origin. Do not implement
  peer consultation by submitting its text as a new person message.
- Hosted session construction is supplied through the existing boot boundary.
  Avoid introducing recursion or lock coupling when resolving another session.
- SQLite schema v2 adds immutable context revisions. Later migrations still need
  atomic upgrade and refusal/rollback tests; never recreate an existing database.
- This draft settles informational records and explicit applicability, not an
  accepted-decision authority model. Consultation lifetime, discovery limits and
  adoption controls remain open. A source address is not user acceptance.
- Context targets may name ongoing items or files, but only chats and ordinary
  task workers receive automatic turn snapshots. Scheduled firings, forked hands
  and adaptive nodes are not integrated. Global semantic discovery and autonomous
  inter-chat consultation remain absent.

## Coordination and workspace safety

The shared checkout at
`/Users/santoshkumar/Documents/agentfield/code/aforge-v2` contains another
session's work. Do not stage, stash, reset or clean it. Work in this branch's
worktree and stage explicit owned files.

The conversation integration owner is Codex task
`01a07817-8da4-7ed1-a5ae-9ac6980d956f`, titled
“Continue 01a071d3-509d-79b2 (2)”. It maintains
`/private/tmp/af-runtime-next`; do not overwrite its binary or checkout.
The owner has been notified about this separate draft continuation.
General-harness PR #660 subsequently merged into `dev` and has been integrated
here. The owner was notified about the shared-context completion-reader seam.

The user requested Claude Code CLI with Opus on Spark for parallel work. Resolver,
shared-context storage, worker/E2E and review lanes ran there; Codex integrated
runtime wiring and ran verification locally. Lane output was inspected and copied
through explicit owned paths. No credentials were transferred to Spark.

Product grooming is a separate task, `01a08653-1fcf-7880-b64f-dae46f29b86a`, on
`codex/personal-ai-grooming`. Its [draft #663](https://github.com/Agent-Field/aforge-v2/pull/663)
targets this backend branch and owns `grooming/`. Keep it separate; this backend
wave neither merges it nor dispatches later slices. Its five-domain acceptance
brief informed the functional cases. Do not overwrite its documents.

## How a new chat should resume

1. Read this file, the two linked design documents, current `AGENTS.md`, and
   relevant `docs/changes/unreleased/` entries. Check branch, worktree, diff and
   PR state; this document may lag an interrupted in-progress edit.
2. Preserve the draft/no-merge instruction. Confirm the latest upstream and
   concurrent PR changes before integrating them. Do not move to old `main` or
   replace this work with the shared checkout's dirty tree.
3. Reconcile the status table with actual code and tests, then continue the first
   unresolved dependency. Prefer functioning integrated slices to unconnected
   scaffolding or more presentation work.
4. Exercise the representative journeys in `CONSTRAINTS.md`. Test unavailable
   sources, memory disabled, independent task addresses, scope/authority, restart
   and retry behavior where relevant. Never substitute fake success for an
   unwired capability.
5. Build with `make build` in this worktree. Record exact checks, results,
   unresolved failures, review binary and commit. Keep the draft PR and this
   document current; leave the branch available for user testing.

## Current checkpoint and next action

The integrated backend and the completion follow-up are pushed on draft #662;
none of this draft was merged into `dev`. The current production changes are
through `3f678b893`; later commits strengthen functional assertions. The complete
session/manual/untagged E2E regression passes on that production revision.
The earlier ordinary PR gate at `743f70866` passed all touched packages and laws;
check the latest head separately before relying on CI status.

The follow-up captured and repaired missing write evidence in the completion
reader. The newest completed write/edit carries bounded whole arguments beside
its own result, without evicting earlier failures. Completion objections are
observations to check, not instructions to alter correct work blindly. File-tool
arguments now prefer existing workspace-relative resolution; full absolute
paths remain the rule for references shown to the person. Explicit paths and
approval boundaries retain their meaning.

The new complete working-day run passed on `3f678b893`: twelve chats, concurrent
submissions, eight long review turns, revisions, idle behavior, reopening,
membership removal/rejoin and withdrawal. The worker and conflicting-source
reruns passed on `fb95cd197`, including both requested task artifacts returning.
Read [COMPLEX-TESTS.md](COMPLEX-TESTS.md) for immutable receipts, earlier failures,
interrupted runs and the distinction between model judgments and backend state.

**Reliability is still an open item.** [#672](https://github.com/Agent-Field/aforge-v2/issues/672)
remains open because real completion probes have both passed and falsely judged
controlled evidence on the repaired code. A green working day is valuable but
not proof that every later model judgment will be correct. Keep positive and
negative probes strict; do not replace actual delivered-file assertions with
assistant claims. `c9b9ff388` additionally parses the unrelated chat's scope JSON:
old domain receipts proved prompt isolation but did not validate that file.
All five domain journeys then passed this stronger assertion on `c9b9ff388`;
their individual receipts and limits are recorded in COMPLEX-TESTS.md.

The paid CI workflow remains **NOT RUN** when its repository credential is
unavailable. Local real-model runs are recorded separately; skipped paid CI is
not a passing live suite. Commands are in [FUNCTIONAL-TESTS.md](FUNCTIONAL-TESTS.md).

Keep #662 draft and unmerged. The next reliability work is grounded completion
judgment and usable delivered artifacts, followed by a full strict live run on
one reviewed revision. The remaining product slices in the table above are
still pending: addressed consultation, ongoing activation integration,
discovery beyond links, and learning/proposed work. Groom each before expanding
implementation. Do not add a second scheduler or jump to the dashboard to cover
missing backend behavior. Product-grooming draft #663 remains independently owned.
