---
kind: fixed
title: Continuing a chat reconciles spending by call identity and keeps late receipts in its home
pr: 2332
surface: [chat, engine]
invalidates:
  - A take used to assume every ledger row covered the next saved call; new saved calls now share the live row identity and are appended only when absent.
  - Late receipts used to resolve the current home when accounting arrived; each chat now keeps the ledger path captured when it opened.
---

A new shared-identity receipt is marked in the ledger so it no longer hides
another saved call, including an older saved call in a mixed chat. Every live
usage producer now receives that exclusion at the shared recorder, including delegate and fallback memory charges
that supply no call identity. Historical imports use a separate entry point so
unidentified old calls still cover their settled prefix. A queued live row
and a take import of that call produce one ledger row, including when writers
run in separate processes on the same machine. Separate calls with identical
model, tokens and cost retain separate identities. Pricing and spend calculations
are unchanged.

The usage ledger gains an optional `shared_id` saved-format field; older builds ignore it and read the same dollars and rows.

The saved call identity is optional. Existing transcripts, ledgers and receipts
load without migration. Calls without a shared identity retain the previous
ordered-prefix fallback for settled rows; queued or out-of-order legacy rows
cannot be reconciled exactly. Unmarked rows from earlier builds, including
independent ledger IDs, remain ambiguous when they are not old-call coverage.
Accounting can finish after a conversation closes without creating a bookkeeping
turn or writing into another home. Process shutdown stops chats and waits for
admitted receipts concurrently, including already closed chats and retired task
children, before draining the ledger. Successful Factory answers awaiting cost receipts belong to this wait
without delaying the next Factory step. This wait follows the provider's
70-second bound; timeout warns that
spending may be incomplete and lets quit proceed.

Ordinary ledger writers reuse cached identities only while their ledger
generation is unchanged. External growth or modification rebuilds the index, including when an
interior restore introduces a previously uncached identity. Both present and
absent identities are checked against that rebuilt generation. This can require
a full scan when writers alternate across processes. Rotation reopens the active
path and retries the same charge rather than losing it or writing to the archive.

Mirrored remote charges retain their event identity through transcript digestion
and take imports, even alongside ordinary sealed calls. Each receiving ledger
compares the remote event independently of its row ID and provenance marker,
including older rows that retain the remote event. Imports and live replay recognize the same event;
remote rows cannot cover an unidentified ordinary call. Previously a remote-only
round trip could repeat spending, while one ordinary receipt could hide all
remote spending from the import. Earlier imports that discarded the remote
event cannot be repaired automatically: their unidentified rows remain ambiguous.

Mirrored remote charges also invalidate cached event membership after inode,
size or modification-time changes. Removed events can be booked again and events
already present in a restored ledger are suppressed. This fixes both missing and
repeated spending after remote-ledger restoration; alternating external writers
can require a full scan here too.

The retired-child receipt regression now uses the engine's routed title role,
so it admits a real receipt before asserting family settlement and exact cost.

First-write identity scanning now streams rows with a reused decode buffer
instead of retaining a full decoded history. The million-row regression checks
the existing flush deadline, exact totals and bytes, and replay suppression;
no flush or close deadline was extended.

The nonblocking writer regression holds a regular ledger’s file lock, then
checks that releasing it durably drains every queued and overflow charge.

Usage writers now validate and append up to 128 already queued rows under one
ledger lock. Quiet writers still append immediately. This removes per-row
prefix hashing, descriptor setup and identity decoding from overflow draining;
the existing 20,000-charge race regression completes within the unchanged flush
deadline. Batch-local and historical identities remain deduplicated under that
same lock, including ordinary imports and mirrored remote events.
