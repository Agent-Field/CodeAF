---
kind: added
title: owned task layouts and explicit multi-root relay mapping
pr: 1944
surface: [chat, engine, remote, docs]
invalidates: []
---

Give supported delegated tasks private layouts and reuse the admitted layout for
exact cooperating work. Carry multiple working roots with stable identities,
versioned directory metadata, restart receipts and independent fork history.
Ask for receiver-local folders before acquiring the source lease; cancellation
and invalid mappings preserve the existing holder and allow a valid retry.

Local archive recovery requires the separately reviewed native dependency.
Uncertain writers and multi-root reclamation remain excluded. Existing
conversations require compatibility safeguards; this change does not silently
rewrite legacy task histories or promise long-term storage savings.
