---
kind: fixed
title: Continuing a chat reconciles spending by call identity and keeps late receipts in its home
pr: PRNUMBER
surface: [chat, engine]
invalidates:
  - A take used to assume every ledger row covered the next saved call; new saved calls now share the live row identity and are appended only when absent.
  - Late receipts used to resolve the current home when accounting arrived; each chat now keeps the ledger path captured when it opened.
---

A same-chat late receipt no longer hides another saved call. A queued live row
and a take import of that call produce one ledger row, including when writers
run in separate processes on the same machine. Separate calls with identical
model, tokens and cost retain separate identities. Pricing and spend calculations
are unchanged.

The saved call identity is optional. Existing transcripts, ledgers and receipts
load without migration. Calls without a shared identity retain the previous
ordered-prefix fallback for settled rows; queued or out-of-order legacy rows
cannot be reconciled exactly.
Accounting can still finish after quit without creating a bookkeeping turn or
writing into another home.
