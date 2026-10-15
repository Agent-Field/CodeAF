---
kind: fixed
title: a report that opens with a code fence quotes its sentence on every surface, not only the landed card
pr: 1807
surface: [chat, engine]
invalidates:
  - >-
    #889 was believed to have fixed the three-backticks quote everywhere. It
    fixed only the landed card. A report that opened with a code fence still
    drew `` ``` `` on the `/tasks` table, the home panels and the task record,
    because `taskOutcome` in internal/session read the literal first line while
    the card read the first PROSE line. Every surface now reads a report through
    one shared reader, internal/reportline, so the fence is skipped once and for
    all of them.
---

A task that answers with a diff, a command's output or a JSON block opens its
report with the code fence around that answer, and a fence marker says nothing.
The landed card learned to skip it in #889, but the card was not the only path:
`taskOutcome` wrote `TaskIndexEntry.Outcome` — the field the tasks table, the
home panels and the task record all quote — from the literal first line, so the
same work landed with a sentence on its card and three backticks on its row.

The two readers could not reach each other: `firstProseLine` and the fence
detector it leaned on, `mdFenceOpen`, were private to internal/tui3, and
`taskOutcome` lives in internal/session. The fence-marker detection and the
first-prose-line reading now live in one leaf package, `internal/reportline`,
that both the surface and the engine import. `taskOutcome`, `taskFirstSentence`,
`switcherFirstLine` and the renderer's `mdFenceOpen` all route through it, and a
structural law pins that they do, so a later hand cannot reach for `firstLine`
and put the fence back on a surface. No wording changed: a row that quoted a
sentence still does, and a row with nothing to quote still draws nothing.
