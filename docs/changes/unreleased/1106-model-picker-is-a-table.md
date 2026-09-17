---
kind: changed
title: The model picker is a table with headed columns, not a ragged tail of facts
pr: 1106
surface: [chat]
invalidates:
  - "A `/model` row drew its facts as one right-aligned `·` tail — `$0.06/$0.18 per M · 131k · sees · watches` — so no two rows put their price, window or score in the same column and the list could not be read down. From sixty columns up the facts are a TABLE now: `via`, `first`, `in/M`, `out/M`, `window`, `t/s`, `elo`, `reads`, `makes`, each under a dim heading line, each cell in its own column. Under sixty columns the ranked tail is unchanged and is what still draws."
  - "The capability verbs are gone from every row. `sees`, `hears`, `watches`, `draws`, `speaks` and `films` are replaced by the catalog's own nouns under two heads that name the side: `image`, `audio`, `video`, `file` under `reads`, and `image`, `speech`, `audio`, `music`, `video` under `makes`. `speaks` had folded speech, audio and music into one word and no longer can. A tail with no head to lean on — `codeaf models`, a narrow frame, a phone — spells the side out as `reads image, video · makes image`, which is what `tui3.ModalityWord` returns now."
  - "A column the list was FILTERED ON is not drawn when every row of it agrees, which is why `makes` is absent nearly everywhere — `image` on all fifty-four rows of the drawing slot is the slot's own name written once per row. It is asked of `reads` and `makes` only (`modelColumn.selects`), because a price or a window that agrees across a short list is a coincidence rather than a definition, and the emptiness law may not grow into hiding facts that were published."
  - "`sees` and `draws` survive in exactly one place: the picker's filter grammar, where they are still the words you type. They stayed verbs because `image` is a word dozens of model ids carry, so accepting it as a modality term would break the id search for `qwen/qwen-image-3`. The row and the filter box therefore no longer use the same words, which they used to."
  - "`/model <slug>` on a model that cannot hold a conversation said `— it speaks.`; it says `— it answers with speech.` now, built from the same reading (`cannotChatBecause` in `internal/tui3/palette.go`). It also answers for families the verbs never covered — `— it answers with transcription.` — where it used to give the bare sentence."
  - "The order the modalities are said in is codeaf's, not the catalog's: the live rows publish the same set as `text, image, file`, `file, image, text` and `image, text, file` on neighbouring models, so echoing the published order put one fact in three places down a column. A modality this build does not know is still drawn, after the ones it does, sorted."
  - "`modelNameWide` — the most the name column may demand — is 38, not 44. It was measured against a live 355-row catalog where 343 ids are 36 cells or fewer; at 44 the twelve outliers cost every row the whole `reads` column."
  - "The picker rows no longer spell the unit on every row: the heading carries it. `$0.09/$0.18 per M` is `$0.09` under `in/M` and `$0.18` under `out/M` (two columns now, not one string), `elo 1290` is `1290` under `elo`, and `58t/s` is `58` under `t/s`. A test or a screen-scrape matching those old spellings on a wide frame will miss; `internal/tui3.modelNote` and the tail path still write them, and so does `codeaf models`."
  - "A picker row's tail could be shorter on one row than on its neighbour, because each row spent its own cells. Every row now draws the same columns, so an empty cell means the catalog published nothing and can mean nothing else — and a column no row on the list published is not drawn at all, heading included."
  - "`picker.headLines` takes a width now and counts the table's heading; the heading is charged OUTSIDE the twelve-row ceiling, so twelve models still show. A test that asks `picker.rows` for exactly `len(hits)` lines gets the heading and one model fewer — ask for `len(hits) + p.headLines(width)`."
  - "The manual said the six capability words — `sees`, `hears`, `watches`, `draws`, `speaks`, `films` — were what a model row may say, without saying that only the three INPUT words can ever appear in `/model`: a model publishing any non-text output modality is not a conversation model and is off that list entirely, so `draws`, `speaks` and `films` only ever show in the media slots on the Providers tab. The page says which is which now, and that the `can` cell reads published modalities only and never infers from the id — the **looking** slot does fall back to `vl`/`vision` in a name, so a silent `…-vl` row can be offered there while showing nothing under `can`."
  - "The tilde on a floating model id had one heading covering it and `…-latest` together, and neither `why are some model names prefixed with a squiggle` nor `why does this model start with ~` reached it. `internal/manual/chat/lanes.md` splits them: `Why a model name starts with ~` is its own section, `A model name that ends in latest` keeps the rest, and the unmeasured-model paragraph that was buried in the unpinning section has its own heading too."
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
