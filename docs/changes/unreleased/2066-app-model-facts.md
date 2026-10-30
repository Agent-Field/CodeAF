---
kind: added
title: codeaf app lists each model's prices, context length and Arena Elo
pr: 2066
surface: [engine]
invalidates:
  - "Was true: a `models.list` row said only a model's id, name, provider, default and listing time, so the app could not show what a model costs, how much it holds or how it scores. Is true now: a row also carries `price_in_per_m` and `price_out_per_m` (US dollars per million tokens), `context_length` and `arena_elo` from the catalog, each absent when nobody published it. An unknown price is never sent as `0`."
---
