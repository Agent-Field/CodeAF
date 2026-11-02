---
kind: fixed
title: codeaf counts memory spending and keeps job learning inside its turn
pr: 0
surface: [engine]
invalidates:
  - "Memory compiler costs were kept only in its private receipts, so Spending and job headers understated provider spending. Memory calls now enter the usage ledger with the memory role, and owned calls charge the job's existing meter."
  - "The process worker could import job conversations and continue compiling them after Pause or completion. Job sources now belong to the job turn and are excluded from independent background compilation. Idle polling no longer launches refinement without pending sources."
---

Saved knowledge, citations, search and page organization remain available.
A finished job turn runs one compilation before its event stream closes; Pause
cancels that work. Compilation can require multiple dependent model requests
for extraction, organization and evidence review. Unchanged input reuses validated
receipts. Existing historical ledger gaps are not reconstructed.
