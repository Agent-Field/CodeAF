---
kind: fixed
title: the spend lab opens on the fixture's clock, not the wall's
pr: 526
surface: [chat]
---

Two spend-place tests — the one that draws the ledger it walked in on, and the one that
keeps the paging control over an empty window — went red on a clean tree at midnight on
2026-09-03, with no change behind them. The lab that builds them wrote its lines on fixed
August 2026 dates and then opened the place on the wall clock, and the place windows the
ledger with `session.LastDays(now, 14)`: the $21.40 line on August 20 simply aged out of
the fortnight. The lab now pins the app's clock to the fixture's own moment, which is what
the sibling helper on the everyone-walk already did, so the window and the lines agree
whatever day the suite runs on. Nothing a person sees moved.
