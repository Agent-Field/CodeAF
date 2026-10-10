---
kind: added
title: Place graph stores knowledge lines with provenance and Undo
surface: [engine, desktop, docs]
invalidates:
  - "The place graph stored instructions only as one prose field. Existing documents now migrate paragraphs once into provenance-bearing knowledge lines, leaving the compatibility field empty and the source list intact."
---

This lane adds storage and receipt-backed line writes. Home editing, chat tools,
contradiction detection and stale-line prompts require their integration lanes.
