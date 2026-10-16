---
kind: added
title: codeaf app reads and saves the spending ledger by run, with exact totals
pr: 0
surface: [engine]
---

Was true: the app's only door onto the usage ledger was `spend.export`, which
writes every raw call in a range to a CSV under `<home>/app/exports/`; nothing
paged the ledger, grouped it by execution, filtered it, or wrote where the
person asked.

Is true now: `spend.ledger` answers a page (default 200, at most 500) of the
ledger for local calendar days, with the whole range's totals, and
`spend.ledger.export` writes the same filtered rows as CSV to an absolute
destination — atomically, mode 0600, never over an existing file without
`overwrite`. A row is a run (every call one execution made, recorded since
`UsageLine.run` existed) or a single call that carries no run; older,
auxiliary and late calls are never grouped into a pretend run, and unpriced
calls are counted, not shown as $0. Both methods are optional: an engine
without them answers `unknown_method`. `spend.export` is unchanged. Every
request reads the ledger linearly from the start; a range with more than
100 000 runs is refused, and a third concurrent reading answers `busy`.

A page holds the page: the runs are added up under that budget (an estimate,
not a resident-memory promise) and the page is chosen from them without
sorting them all. One answer is at most 2 MiB of JSON and a larger one is
refused, asking for a smaller limit or narrower filter, never cut. Figures that
would overflow or leave the finite floats are refused. A cursor pins the bytes
the first page read, so appends never move a paging, and a ledger rotated, cut
or rewritten under it refuses the cursor. Names come only from what the app
holds for the rows answered, so an older conversation the app is not holding is
unnamed. Without `overwrite`, a folder that cannot make hard links is refused
rather than checked and renamed. CSV costs are full precision, not rounded to
four places.
