---
kind: changed
title: The model picker is a table with headed columns, not a ragged tail of facts
pr: 1106
surface: [chat]
invalidates:
  - "A `/model` row drew its facts as one right-aligned `·` tail — `$0.06/$0.18 per M · 131k · sees · watches` — so no two rows put their price, window or score in the same column and the list could not be read down. From sixty columns up the facts are a TABLE now: `via`, `first`, `in/M`, `out/M`, `window`, `t/s`, `elo`, `can`, each under a dim heading line, each cell in its own column. Under sixty columns the ranked tail is unchanged and is what still draws."
  - "The picker rows no longer spell the unit on every row: the heading carries it. `$0.09/$0.18 per M` is `$0.09` under `in/M` and `$0.18` under `out/M` (two columns now, not one string), `elo 1290` is `1290` under `elo`, and `58t/s` is `58` under `t/s`. A test or a screen-scrape matching those old spellings on a wide frame will miss; `internal/tui3.modelNote` and the tail path still write them, and so does `codeaf models`."
  - "A picker row's tail could be shorter on one row than on its neighbour, because each row spent its own cells. Every row now draws the same columns, so an empty cell means the catalog published nothing and can mean nothing else — and a column no row on the list published is not drawn at all, heading included."
  - "`picker.headLines` takes a width now and counts the table's heading; the heading is charged OUTSIDE the twelve-row ceiling, so twelve models still show. A test that asks `picker.rows` for exactly `len(hits)` lines gets the heading and one model fewer — ask for `len(hits) + p.headLines(width)`."
  - "`internal/tui3`'s free `priceField` and `eloWord` are gone. One reading of a model is `modelFactsOf` returning `modelFacts`, which both shapes of row dress: `modelFields` puts the units back on for the tail, `modelFacts.cells` leaves them off for the table. `priceWord`, `contextWord` and `ModalityWord` are unchanged. `lanes.go` gained `laneRateBare` and `laneRateUnit` under `laneRateTight`."
---

Six hundred rows, and the question in front of somebody scrolling them is never
"what does this one cost" — it is "which of these is cheap", "which holds a
million tokens", "which can see". Those are comparisons between rows, and a
comparison needs the fact in the same place on each of them. The ranked tail is
the right shape for a list of settings, where each row's facts are about that row
alone; it was the wrong shape for the one list on this surface that is read down.

The ranking did not change and neither did the facts. `internal/tui3/modeltable.go`
is `modelFields`' own ordering laid out down the page instead of along the row,
giving up its columns from the same end for the same reasons, and falling back to
the tail wherever a frame has no room for columns.
