---
kind: added
title: Resolve organized work and share sourced context across chats
pr: 662
surface: [chat, engine, docs]
invalidates:
  - "Collections used to contain bare references reachable only through the CLI. Chats can now find and organize collections and resolve members through their existing owners."
  - "There was no durable sourced-context record or turn integration. Schema v2 adds immutable revisions, withdrawal and explicit targets; chats and ordinary task workers refresh bounded information independently of learned memory."
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
