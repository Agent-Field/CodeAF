---
kind: fixed
title: /cost and every multi-row note keep their columns — the gutter moves every row, not just the first
pr: 1390
surface: [chat]
invalidates:
  - "The indent law's pass in deckRows skipped any work row whose text already opened on two spaces, to keep the fold chip from being indented twice. It no longer looks at what a row begins with: every work row is moved, and the fold chip lays itself flush and takes its two cells from the pass like the rest."
  - "A note's continuation rows (two blanks under the first row's `· `) were left where they were while the first row moved, so /cost's `· spend` stood two cells right of `tokens`, `model calls` and `time`. All of a note's rows now shift together and the label and figure columns line up."
---

The `·` before `spend` is the note's own marker, the dim lane every answer this
surface writes on its own account opens with; nothing else prints there. It now
sits in the gutter column with the table's columns intact under it.
