---
kind: changed
title: the manual's stemmer meets itself by Porter's measure, and "much" is a stop word
pr: 458
surface: [chat, engine]
invalidates:
  - "The manual's stemmer stripped -ed and -ing but left a base form's final e, so a question saying `refuse` never met a heading saying `refused`, and `size` never met `sizing`. Both now stem to one string: a one-measure stem ending consonant-vowel-consonant gets its e back, and a base form loses its e only above one measure."
  - "Stripping every final e looked like the fix and was measured wrong: it merged paste with past and bare with bar. internal/manual/stem_test.go pins the pairs that must meet and the distinct pairs that must not."
  - "`much` scored as evidence in a lookup, so \"how much has this conversation cost\" reached the context-window section. It is a search stop word now, beside \"how\" and \"what\"."
---

Measured on the frozen question sets: plain first 21/25 → 21/25, held-out first 7/22 → 8/22, within-four unchanged; two questions moved up, two moved one place down and stayed within the four.
