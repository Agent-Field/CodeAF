---
kind: fixed
title: isOpenRouter reads the alias marker off the model before the prefix check
pr: 470
surface: [engine]
invalidates:
  - "A model spelled with the alias marker — `~openrouter/…` — behind a non-openrouter base turned the lane path off silently, because `Client.isOpenRouter` read `config.Model` raw and the leading `~` failed the `openrouter/` prefix check. `isOpenRouter` now normalizes the model first (`normalizeModel` strips the `~` and lowercases), so the alias-marked and bare spellings route identically."
---
