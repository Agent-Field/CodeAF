---
kind: added
title: RoleEmbed is a pin and discovery has a real /embeddings adapter
pr: 1211
surface: [engine, chat]
invalidates:
  - "There was no embeddings client on the provider. OpenAI-compatible POST /embeddings now rides the existing media client, spend-tagged `embed`, never RoleAuditor."
  - "RoleEmbed did not exist, so a pin for an embeddings model was dropped. `embed:` is a vocabulary pin like `imagegen` and `speech`, not a text-tier Register, configurable through `roles.embed`."
  - "A down embedder had no labelled path. Expanded-query lexical retrieval is now `degraded` / `discovery delayed`; it does not replace vectors, does not file, and does not say the workspace was checked."
---

Wave 2 embed lane. Spark's catalog on 2026-09-18 served
`openai/text-embedding-3-small` (inspected without printing keys); that slug is
the curated default, not TUI copy.
