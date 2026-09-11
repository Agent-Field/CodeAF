---
kind: fixed
title: a task card quotes the report's first line that says something, not its literal first line
pr: 889
surface: [chat]
invalidates:
  - "The landed card's quoted sentence was the report's literal first line (`firstLine(node.report)` in `internal/tui3/taskdone.go`), so a report that opened with a code fence drew \"```\" as its outcome. It is the first line that says something now: leading blank lines and fence markers — ``` and ~~~, with or without a language word — are skipped, a report that is nothing but a fenced block quotes the first line inside it, and a report with no prose at all draws the stamp alone rather than empty quotation marks. `internal/tui3`'s `firstProseLine` is the one reader, and every road that fills a card's quoted half goes through it."
  - "The card's row was believed to quote `session.TaskNotice.Report` verbatim from its first byte. It never did anything so simple: the quote is a reading of the report, and the reading now knows what a fence is."
---

A quick task landed on 2026-09-11 (`dev@333acc67d`) whose report opened with a
fence around its answer, and the card drew `"```" · started 10:45 · ctrl+o
output` — the one row that exists to carry the work's own sentence, spent on the
punctuation around it.

The reports are the model's own markdown and a fence in front of an answer is
not a defect in them: anything that reports a diff, a command's output or a JSON
result is *correctly* fenced. The defect was the surface reading the first line
literally, which is a reading that only works for prose.

The quoted half is now the first line of the report that says something —
`firstProseLine` in `internal/tui3/render.go`, sitting beside the `mdFenceOpen`
it borrows from, so the two agree on what a fence is. A report with no prose at
all keeps the emptiness law it always had: the row falls back to the subtitle
and, failing that, draws the start stamp alone rather than an empty pair of
quotation marks.
