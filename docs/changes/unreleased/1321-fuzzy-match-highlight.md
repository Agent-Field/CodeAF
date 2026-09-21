---
kind: changed
title: quick-search rows draw the matched letters in bold, and internal/fuzzy says where each term hit
pr: 1321
surface: [chat]
invalidates:
  - "internal/fuzzy answered scores only: no caller could ask where a term matched. It now records its DP decisions as it runs and backtracks the best cell, and ScoreHits and ScoreFieldsHits return per term the winning field and the matched byte indices into caller-reused slices — the optimal span: `foo` against `xf foo` lights the whole word after the space, not fzf's `xf_oo`."
  - "The settings sheet's rows and the model picker's rows were plain ink: nothing on a row said which letters the query hit. Matched runes now draw bold over the row's own colour through the existing token system — no new colour, no background fill, nothing dimmed — and each term lights only in the field it won, so `ask prompt` bolds `ask` on the label and leaves the value's `prompt` alone; a row with no query paints byte-identically to before."
---

The positions come from backtracking the DP that scored the row — the came-from
byte each cell already recorded — and not from a second forward pass, so the
keystroke hot path still allocates nothing per row. Some matches highlight
nothing, deliberately: a term past the needle guard, a field that had to be
case-folded whole, and a lineage whose prefix its own gap costs floored all
come back matched with an empty span, because bolding bytes that are not the
alignment the score paid for would lie about why the row ranked.
