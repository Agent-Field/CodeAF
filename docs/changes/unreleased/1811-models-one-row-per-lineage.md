---
kind: fixed
title: App model picker shows one concrete row per lineage
pr: 1811
surface: [engine, docs]
invalidates:
  - "GET /app/v1/models listed router variants, floating aliases and dated builds as separate choices. It now shows one row per lineage, preferring the plain undated id and otherwise the newest dated build."
  - "Saved variant, alias or snapshot ids added duplicate picker rows with raw ids for names. A listed lineage now represents those saved choices; a choice with no listed lineage keeps its unavailable row and remembered catalog name when known."
---

Filtering belongs only to the app model listing. Model writes and first-launch
setup retain the provider's full catalog, including variants and snapshots.
Saved default and pinned ids keep their exact spelling in the response.
