---
kind: renamed
title: the /connect row for a custom service reads Custom OpenAI-compatible API, not Something else
pr: 1107
surface: [chat, docs]
invalidates:
  - "The `/connect` row (and its Providers twin in `/settings`) that adds a custom connection was called **Something else**. It reads **Custom OpenAI-compatible API** now, in the row, in the manual's services and commands pages, and in the connect tests that name the row. The connection id is still `custom`, so stored connections, their model ids and renames are untouched by the wording."
---

The old name said nothing about what the row does; the new one says it
exactly. Nothing else in the flow moved.
