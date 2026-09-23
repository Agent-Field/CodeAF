---
kind: fixed
title: a Codex model carries its 272k window, and a Codex call row names Codex as its endpoint
pr: 1402
surface: [chat, engine, docs]
invalidates:
  - "A Codex model had no context window: the account listing's `context_window` was dropped, so a conversation that moved onto `codex/<slug>` kept its previous model's figure on the status line and for compaction. Every Codex model now carries the listing's window, `272k` today, and the four fallback rows carry the same figure."
  - "A session asked the catalog of the service it STARTED on for a switched-to model's window, so a move onto another service's model found none, and over an engine host nothing else set it. The window is now read from the new model's own service, on the engine as well as in-process, and the programs a conversation runs are sized from the same figure."
  - "A Codex call row in the transcript named no endpoint. It names `Codex`, declared by the Codex service row (`ServedAs`) and written only into call rows; Codex answers still carry no server name, so nothing learns or draws Codex as a router lane."
  - "The model warm was handed the catalog a conversation started on and wrote it into the default model list, so a launch on `codex/<slug>` replaced the OpenRouter list with bare Codex ids. It now always warms and writes the default service's own catalog."
---

A profile connected to Codex by an earlier build reads the `272k` figure on its next launch
without connecting again: rows remembered without a window take the fallback's observed
figure for the four models it names, any other id stays unknown, and a later listing's
true figure replaces the fallback's. Fixes #1383 and #1391.
