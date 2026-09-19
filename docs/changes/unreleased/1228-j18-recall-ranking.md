---
kind: fixed
title: hybrid search ranks original passages despite different wording
pr: 1228
surface: [chat, engine]
invalidates:
  - "SearchEvidence concatenated lexical hits first, so a paraphrase of the query could occupy every top-20 slot. It now gathers a wider BM25-OR and embedding pool, ranks one session list, and treats restatements of the query as not the original passage."
  - "SearchLexical AND-matched every query token on one passage, so a short correction and a buried mixed-topic note missed. Terms are OR-ed and ordered by BM25."
  - "The search place read graph.db lexical-only. With discovery open it uses the same SearchEvidence ranking; an empty evidence list still falls back to the conversation index."
---

J09/J18 A4 is original authenticated-access passages, not chats that restate
the emailed-receipt ask. A7 and global still have to beat a topical majority.
Live 10k re-eval is a later pass; this change is the production ranking and
its tests.
