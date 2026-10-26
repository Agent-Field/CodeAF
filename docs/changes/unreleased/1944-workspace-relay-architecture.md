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
Typed folder targets survive asynchronous listings; confirmation waits for the
listing instead of choosing an unloaded parent or the first sibling.

Review and apply each writable root explicitly through the terminal. Application
uses the exact reviewed snapshot and retains the original private copy; edits
after review do not land. Conflicts preserve both versions. Completed plan workers
reopen their owned transcripts after restart or relay; local index rebuilding
recovers their recorded model without retaining another computer's file paths.

Local archive recovery requires the separately reviewed native dependency.
Uncertain writers and multi-root reclamation remain excluded. Existing
conversations require compatibility safeguards; this change does not silently
rewrite legacy task histories or promise long-term storage savings.

Document the agent contract for declaring tool roots, checking admitted worker
routes, mapping received roots, reviewing results and cleaning owned fixtures.
Current acceptance evidence is maintained in the pull request rather than as a
pending status in the architecture reference.

Optional warm-transfer frame hints now travel through the shared Raft rules.
Invalid hints are omitted without blocking a saved head; Home listings omit them.
The prototype remains off by default, and every cluster member must understand
these fields before a device sends them. Receiving multiple task roots preserves
the captured draft and pending send through folder selection, records the acquired
send fence, and keeps cancellation and failed moves from sending on an older copy.
