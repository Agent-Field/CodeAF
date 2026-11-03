---
kind: changed
title: codeaf reuses exact daily spending rollups between reports
pr: 0
surface: [engine, app, docs]
invalidates:
  - Every spend.report request scans the complete usage ledger.
---

Date-only `spend.report` requests now build an in-memory daily rollup and read
only complete appended lines on subsequent requests. Late receipts keep their
original day. File identity changes, shrinkage and changes to the already-read
prefix's ends rebuild the rollup, using the ledger's existing identity and
prefix checks. Concurrent reports serialize their reads. No file is added.

The rollup holds daily model and subject groups, never individual calls, with
at most 8,192 groups and an estimated 4 MiB of cells across one retained ledger.
Reports retain their existing budgets, ordering, totals and gap statistics.
Additional filters, partial days, zone changes, stored-day disagreements,
invalid figures and exhausted cache storage use the original scan. Costs also
use that scan unless regrouping is provably exact: ordinary decimal amounts
can change floating-point rounding when grouped, so these are not accelerated.
