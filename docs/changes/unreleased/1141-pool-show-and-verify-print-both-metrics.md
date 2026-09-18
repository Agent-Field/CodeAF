---
kind: changed
title: pool show and verify say both metrics the index carries
pr: 1141
surface: [engine]
invalidates:
  - "`codeaf pool show` printed the held index as a metric count (`… 2 metrics …`) and `codeaf pool verify` ended its sentence with the same count, so the relay's second metric was invisible and a person could not tell which cells are judged scores and which are graded shares. show now prints one line per declared metric after the index line — `role_quality: gaussian score · 12 cells · dims role, model`, and for the metric whose cells are split by the grader or judge that produced each share, the distinct sources beside them: `acceptable: bernoulli share · 7 cells · dims role, model, source · sources reviewer, grader`. verify says `metrics role_quality, acceptable` where it counted."
  - "The `--json` answers of show, status and verify carry `metric_list` — one object per declared metric with `name`, `kind`, `unit`, `dims`, `cells` and, where the cells are split by one, `sources` — beside the `metrics` count, which stays an integer: the count is what a reader of today's shape already reads, and the array is the detail added beside it, not instead of it."
  - "`internal/pool/index` read a metric's `unit` from the document and dropped it unread. `Unit(metric)` and `Dims(metric)` now answer the words the document spells — the unit folded like `Kind`'s word, the dims the metric declares beyond role and model, sorted — and a one-metric document, the seed's, prints one line."
---

The line shape follows the index's own order, sorted, and a metric the
document spells no unit for says its kind alone. The sources are gathered
off the cells, so a metric without a `source` dim says none, and a source
the cells spell two ways is matched the way the index matches a name —
lowercased, trimmed — and said once. `verify --json` carries the same array
beside its count, so both doors answer the same shape.
