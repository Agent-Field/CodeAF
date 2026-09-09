---
kind: added
title: Resolve organized work and share sourced context across chats
pr: 662
surface: [chat, engine, docs]
invalidates:
  - "Collections used to contain bare references reachable only through the CLI. Chats can now find and organize collections and resolve members through their existing owners."
  - "There was no durable sourced-context record or turn integration. Schema v2 adds immutable revisions, withdrawal and explicit targets; chats and ordinary task workers refresh bounded information independently of learned memory."
  - "The completion reader previously judged report fields from a clipped write label and a byte-count receipt, and its objection was presented as fact. The newest bounded exact write/edit input now accompanies its result; missing evidence is explicit and objections must be checked against current work before editing."
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
semantic discovery, shared-context scheduling, or a dashboard redesign. The
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
