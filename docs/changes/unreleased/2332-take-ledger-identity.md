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
another saved call, including an older saved call in a mixed chat. A queued live row
and a take import of that call produce one ledger row, including when writers
run in separate processes on the same machine. Separate calls with identical
model, tokens and cost retain separate identities. Pricing and spend calculations
are unchanged.

The saved call identity is optional. Existing transcripts, ledgers and receipts
load without migration. Calls without a shared identity retain the previous
ordered-prefix fallback for settled rows; queued or out-of-order legacy rows
cannot be reconciled exactly. Unmarked rows from earlier builds, including
independent ledger IDs, remain ambiguous when they are not old-call coverage.
Accounting can finish after a conversation closes without creating a bookkeeping
turn or writing into another home. Process shutdown stops chats and waits for
admitted receipts concurrently, including already closed chats, before draining
the ledger. This wait follows the provider's 70-second bound; timeout warns that
spending may be incomplete and lets quit proceed.

Ledger writers validate their cached prefix before suppressing a replay. A
same-size rewrite invalidates cached identities, and rotation reopens the active
path and retries the same charge rather than losing it or writing to the archive.

First-write identity scanning now streams rows with a reused decode buffer
instead of retaining a full decoded history. The million-row regression checks
the existing flush deadline, exact totals and bytes, and replay suppression;
no flush or close deadline was extended.
