---
kind: fixed
title: session test fixtures join late receipt accounting before removing their home
pr: 2605
surface: [build]
invalidates:
  - "Was true: the shared session test fixture stopped the current home's usage writer without joining pending receipts, so a cancelled call could restart accounting after cleanup. Is true now: fixture cleanup closes the agent, waits for its admitted receipts, settles deferred writes and stops its captured ledger's writer before temporary directories are removed."
---

The fixture now follows the process owner's receipt-before-writer shutdown order.
Deterministic checks hold receipt reconciliation and ledger projection separately,
covering both a recovered charge and a missing receipt's unbilled marker. Product
shutdown behavior and close bounds are unchanged. The fixture retains the existing
bounded fallback when the disk stalls; the regression asserts the responsive case.
