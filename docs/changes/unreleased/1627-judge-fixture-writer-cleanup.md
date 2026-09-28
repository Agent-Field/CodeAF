---
kind: fixed
title: first-run judge fixture joins its usage writer before home cleanup
pr: 1627
surface: [chat]
invalidates:
  - "The first-run judge test could fail after its assertions passed because its usage writer recreated the temporary home during cleanup. It now joins its own ledger writer before removing that home."
---

Addresses another temporary-home writer lifecycle instance of #814, observed
in the aggregate acceptance gate. The pasted-key authorization assertions and
product behavior are unchanged; cleanup owns only the fixture's ledger.
