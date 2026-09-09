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
- Draft PR: creation pending; replace this line once its number is known.
- This continuation currently adds documentation only. No additional backend
  capability is implemented yet beyond the merged foundation below.
- Build this branch with `make build`; its review binary will be
  `/Users/santoshkumar/af-personal-ai-backend/bin/aforge`. A fresh build of this
  continuation has not yet been made. Do not mistake another worktree's binary
  for this branch's output.

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
[IMPLEMENTATION.md](IMPLEMENTATION.md) for the exact first-slice guarantees.
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

### Existing upstream capabilities we should reuse

The starting commit also contains conversation runtime PR #653 and test-feedback
PR #658. These are upstream work, not capabilities implemented by the collection
slice. Existing conversation/task storage, task status projection, standing
instructions and scheduled/semantic checks, origin-aware local delivery, and
hosted session attachment are integration foundations. Their existence does
not establish cross-conversation coordination for collections.

## Remaining implementation and acceptance

All items below are **pending**, unless their status is explicitly updated with
code and verification evidence. The order is a dependency-oriented plan, not a
commitment to add a separate service for each row.

| Slice | Work needed | Evidence required before marking complete |
| --- | --- | --- |
| Resolve organized work | Resolve typed references through their existing owners into useful read views; distinguish missing, unavailable and actual current state. | A collection view reads real chat/task/standing fixtures without creating agents, changing files or duplicating mutable status. Identical task numbers in different chats resolve correctly. |
| Shared sourced context | Settle the smallest representation for shared findings/decisions, their source, revision, withdrawal and explicit applicability. Add compatible migration if storage changes. | One record applies in two places; revision is visible in both; withdrawn or out-of-scope context is not presented as current. Upgrade preserves v1 collections; memory-off works. |
| Runtime context integration | Supply bounded relevant context at real chat turns and task birth; preserve the existing instruction authority and standing scope resolver. | Real runtime assembly consumes current sourced context, rather than a test-only callback. Membership alone cannot inject binding instructions; prompt limits are exercised. |
| Shared consultation | Route an addressed request/reply across related conversations using existing delivery/authority boundaries. Retain one shared exchange or source reference. | Origin remains peer-origin; correlation/retries deduplicate; offline delivery is explicit; a peer cannot change another goal; repeated replies do not cause unbounded paid wakes. |
| Ongoing responsibilities | Connect organized work and relevant context to existing standing items and their activations. Avoid a second scheduler. | A responsibility survives closing its originating chat, can be found from its collection, and handles repeated observations without duplicate action. Email/calendar implications do not grant calendar-write authority. |
| Discovery beyond links | Expose selective discovery over accessible records with source and current state; use explicit signals and semantic retrieval where useful. | An unlinked coding finding can inform a marketing conversation with a traceable source, without indexing similarity as authority or loading all transcripts. |
| Learning and proposed work | Clarify how retained knowledge/methods improve authorized work and how a proposed new responsibility becomes accepted. | Learning does not silently create an ongoing obligation. Existing memory/method mechanisms are reused where sufficient. |
| Backend surface and recovery | Provide the narrow backend entry points needed by chat and later UI; check lifecycle, cancellation, concurrency and restart behavior. | Capabilities work through production assembly in the binary, survive restart where promised, and do not depend on an open TUI. No second task-state owner is introduced. |
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
- SQLite schema v1 has no later migrations yet. Any new version needs atomic
  migration and refusal/rollback tests; do not recreate an existing database.
- Exact shared-record schema, source trust, applicability semantics, consultation
  lifetime, discovery limits and adoption controls still require concrete choices.
  Collection membership alone does not answer them. Record settled choices here
  or in a linked design note before claiming they are accepted product behavior.

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
General-harness draft PR #660 may overlap session execution code; inspect its
current scope and coordinate before changing the same runtime boundaries.

The user requested Claude Code CLI with Opus for parallel implementation.
At this checkpoint a read-only runtime integration reconnaissance lane is
running; no product edits or verification results are attributed to it yet.
Its output is expected at `/tmp/af-personal-ai-lanes/runtime-plan.md`. That path
is temporary: incorporate useful conclusions into tracked documents before
depending on them for a future chat. Do not require access to temporary logs
to understand what was built.

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

Only #661's foundation is implemented at this checkpoint. The new branch is
pushed, and this handoff is being committed before the backend continuation.
Next: finish runtime seam reconnaissance, choose narrow resolver/shared-context
interfaces, and implement the first integrated backend slice. Update this
section and the status table as those changes become real.
