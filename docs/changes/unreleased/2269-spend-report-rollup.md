---
kind: changed
title: codeaf reuses exact daily spending rollups between reports
pr: 2269
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
Reports retain their existing budgets, cell ordering and gap statistics.
Additional filters, partial days, zone changes, stored-day disagreements,
invalid figures and exhausted cache storage use the scan.

Ordinary decimal costs now use the rollup too. Both paths sum each cell's lines
in ledger order, then sum money totals and run-money subtotals in report cell
order. This replaces the scan's former ledger-order money totals. The resulting
rounding differences for ordinary costs are below any displayed cent. Requiring
floating-point addition to be exact under arbitrary regrouping was unnecessary
and prevented real decimal ledgers from benefiting from the rollup.
