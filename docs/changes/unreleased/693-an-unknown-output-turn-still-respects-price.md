---
kind: fixed
title: a first turn nobody has measured an answer for still respects the price of writing one
pr: 693
surface: [engine, chat]
invalidates:
  - "The first turn of a fresh conversation was ranked on the first-token wait alone, at any output tariff. With no answer length stated every lane priced at the prompt alone — the same number on all of them — and `frontierFor` skipped the Pareto front while `underPriceCeiling` skipped the relative price ceiling. An unstated length is now read as `lane.AssumedAnswerTokens` (400 readable tokens, the design's talk figure), filled in once at `Choose`'s entrance, so the front, the ceiling, the price and the score all compare the same answer. Nothing is taught from the assumption: `NoteWorkload` still learns only from completed receipts and `Workload` still answers that it does not know."
  - "internal/manual/chat/lanes.md said a new conversation with no evidence 'starts with unknown answer size' and stopped there. It still has no measurement; the page now says what the absence is compared at, which is what made a first turn landing on the dearest machine unexplainable."
  - "The model picker's `auto` row said `picks the fastest lane each answer`, and internal/manual/chat/models-and-cost.md quoted it twice. It says `weighs speed against price each answer` — auto has weighed both for some time, and on a first turn it now visibly names the slowest lane on the page when that lane is cheap enough."
  - "internal/tui3 stated the talk answer's length itself (`laneTalkTokens = 400`) beside a chooser that states the same figure. It interpolates `lane.AssumedAnswerTokens`, so the row's sort and the chooser's price cannot drift apart."
---

An unknown answer is not a free answer, which is the argument `pricedPessimistically`
already makes a few lines below about an unknown tariff: a zero is a number the rest of
the file compares, and a request whose output costs nothing makes every lane with the same
input tariff look equally cheap. Measured on a fixture of three endpoints differing only
in output tariff — $3, $30 and $300 a million — the unmeasured turn chose the $300 lane
for four tenths of a second, while the same request stating its length chose the cheapest
and refused the $300 lane outright.
