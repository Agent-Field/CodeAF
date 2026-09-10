---
kind: added
title: Resolve organized work and share sourced context across chats
pr: 662
surface: [chat, engine, docs]
invalidates:
  - "Collections used to contain bare references reachable only through the CLI. Chats can now find and organize collections and resolve members through their existing owners."
  - "There was no durable sourced-context record or turn integration. Schema v2 adds immutable revisions, withdrawal and explicit targets; chats and ordinary task workers refresh bounded information independently of learned memory."
  - "The completion reader previously judged report fields from a clipped write label and a byte-count receipt, and its objection was presented as fact. The newest bounded exact write/edit input now accompanies its result; missing evidence is explicit and objections must be checked against current work before editing."
  - "A finishing standing check or action could overwrite a concurrent pause, stop or configuration edit. Revision-aware owner operations and runtime-only writeback now preserve newer intent and completed occurrence progress."
  - "Organization opens no longer reserve the writer for a current schema, and reads cannot recreate a missing database. Task workers can inspect but cannot reorganize or revise shared context."
---

`collections` and `shared_context` are wired through the production binary.
The adapter reads existing conversation, task, ongoing-item and artifact owners;
the collection database does not become a second owner of execution state.
Snapshots preserve the cached system prefix, carry current provenance and
retire stale applicability on reopen. Paging and exact-revision text windows
bound context reads. Identity reads now distinguish globally readable records
from revisions currently applicable to the consumer. A removed membership must
not be restored merely because the completion check can still read the old ID.

`make test-organization-live` builds the binary and runs real DeepSeek V4 Flash
journeys with disposable data. It requires credentials and retains JSON receipts.
The matching CI workflow reports NOT RUN when its repository secret is absent.
The suite also exercises twelve chats with concurrent turns and longer review
history, observed idle behavior, conflicting sources versus a local draft choice,
and a paginated reference beyond the automatic snapshot. These are tests of the
existing behavior, not new activation or accepted-decision semantics.

This is the first integrated backend slice, not autonomous consultation,
semantic discovery or a dashboard redesign. Scheduled context inheritance is added in the governing-context wave below. The
continuation and acceptance record is `docs/design/workspace-foundation/HANDOFF.md`.
PR #662 remains draft and unmerged at the user's request.

Completion evidence distinguishes attempts from success and retains the existing
digest budget. Completion comparisons no longer share the sketch's 300-token
generation ceiling; the existing wall deadline still applies. Real-reader
positive and negative probes accompany the full organization journeys.

The completion follow-up also distinguishes full paths in person-facing file
references from workspace-relative file-tool arguments. The tools already
resolve relative paths; the old blanket full-path instruction encouraged
manual copying of long roots, and live working-day reports landed in mistyped
sibling folders. Explicit supplied/tool-returned paths and approval boundaries
are unchanged. The conflict fixture compares file identity, accepting symlink
aliases while continuing to reject a different or missing report.

The continuation now distinguishes agreed product direction from unresolved
design. An independent real-model behavioral audit retained false-success and
watch-recovery findings alongside passing isolation checks. Its exploratory
fixtures remain on a separate audit branch; they do not establish autonomous
global watching or alter the existing `/land` contract. No audit findings are
claimed fixed by this documentation update.

The current-dev baseline `996117314` is integrated while retaining the draft's
organization/context foundation and newer upstream completion, history and
control behavior. Grooming/design records and the live checklist are consolidated
into this draft; superseded #663 is closed only after its history is retained.

Standing item schema 2 adds stale-write detection and atomic narrow controls.
Runtime writeback preserves configuration, while schedule-specific reconciliation
consumes completed occurrences even after unrelated edits. Old engines/tickers
must be stopped and restarted together before using schema 2; mixed-version writers
are unsupported. This is not external cancellation, rollback or exactly-once effects.
The user has deferred expensive tui3/broad UI acceptance; the checklist records
focused functional Spark receipts and remaining target capabilities separately.

The governing-context wave adds explicit governing folder bindings without
promoting old references, folder-scoped holds with opt-in descendants, and
person-versus-delegated answer receipts. Standing schema 3 prevents older readers
from interpreting scoped rules as project rules; organization schema 3 adds only
empty governing bindings on upgrade. The prior shutdown/restart requirement still
applies. Chat collection tools expose place/unplace/governing; background agents
can inspect but cannot broaden those bindings.

Read-only governing and informational context now follow execution ownership
through child configurations. `context_trace` reads selected-input receipts and
original journal evidence; missing parent causal links and ambiguous overlaps
remain explicit. It does not claim complete causality, instruction compliance or
external-effect verification. All builds/tests in this wave run on Spark, including
targeted checks; no local compilation or tui3 tests are authorized.
