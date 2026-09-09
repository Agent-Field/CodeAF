# Organization runtime: first integrated slice

This describes code on draft PR #662, not the full personal AI design.
The original collection foundation is recorded in [IMPLEMENTATION.md](IMPLEMENTATION.md).
Current verification and remaining work belong in [HANDOFF.md](HANDOFF.md).

```mermaid
flowchart TB
    Door["Binary: local / hosted session assembly"] --> Runtime["Existing session engine"]
    Door --> View["workspaceview: read existing owners"]
    Runtime -->|injected resolver| View
    Runtime -->|collections / shared_context tools| Store["workspace: collections + sourced revisions"]
    Runtime -->|bounded turn snapshot| Store
    View --> Owners["Conversations · tasks · standing items · files"]
    Store --> DB["collections.db, schema v2"]
    Runtime --> Worker["Existing ordinary task worker; organization reads only"]
```

The storage package cannot import execution, UI, provider or optional memory.
The read adapter depends on existing owners; the engine receives its callback
from the binary instead of importing the adapter. Structural tests pin both
boundaries. No new execution engine, scheduler, daemon or owner of task state
is introduced.

## What is retained

A collection keeps its name, stable ID and memberships. A shared record keeps
one stable ID and an immutable sequence of full revisions: title, text, source,
explicit targets and withdrawal state. A pointer identifies the current revision.
Create, revise and withdraw are atomic; stale expected revisions are refused.
Withdrawal is itself a revision. Explicitly revising a withdrawn record restores
it. Each revision attributes the chat that wrote it; prior sources remain in
history. Source identifies a statement, not user acceptance or authority.

The v1-to-v2 migration adds context tables without changing collection IDs or
memberships. An unknown or damaged store is refused. Current-schema opens avoid
reserving a writer. Read paths do not provision a missing database.

Physical transcripts, task checkpoints, ongoing items and files keep their old
owners. Task references are `(owning session, task number)`; file references
remain absolute paths and do not survive renames automatically. Resolution
returns current owner facts or explicit unavailability. Ongoing items currently
resolve active/paused/retired state, not a synthesized state for all their firings.

## How chat consumes it

`collections` finds and groups existing records and resolves a page through its
owners. `shared_context` creates, reads, revises and withdraws sourced information.
Both are conditional on the binary supplying the organization seam. A mutation
that needs a source refuses a conversation with no saved identity. Existing
approval handling still applies. Task workers can inspect, but cannot mutate,
collections or shared context.

On each turn, a chat receives current records explicitly targeting itself or its
direct collections. An ordinary task worker also considers its owning chat and
that chat's direct collections. Membership is one hop; a grandparent collection
and semantic similarity do not imply applicability. Explicit tool queries may
inspect other targets: these are relevance defaults, not information-access ACLs.

Snapshots carry source and revision, label truncated text, and replace earlier
shared-context snapshots. They use the existing volatile tail, preserving the
cached system prefix. Withdrawal or retargeting can be detected from history;
a small session metadata bit remembers exposure after collection membership
removal, including reopen. The bit contains no finding text. Changes refresh at
the next turn, not halfway through an existing provider request. Storage errors
retire confidence in the old snapshot rather than presenting it as current.

See [PERF.md](../../../PERF.md) for storage, page and prompt bounds. List/history
and mutation receipts are metadata; bounded text reads pin revisions between
windows. The store does not duplicate runtime task status.

## Deliberately outside this slice

No autonomous wake, peer consultation protocol, discovery beyond links, event
idempotency, accepted-decision authority model, shared-context delivery to
scheduled firings, or new dashboard. Existing ongoing responsibilities can be
organized and inspected, but this change does not alter how they activate.
Forked hands and adaptive-run nodes do not receive the new organization seam.
Those are separate acceptance slices, not hidden claims of these tests.
