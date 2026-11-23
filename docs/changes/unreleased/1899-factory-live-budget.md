---
kind: fixed
title: Factory budgets cover live owned work and can be adjusted downward
pr: 1899
surface: [engine, chat, docs]
invalidates:
  - "Factory stage costs were previously booked only at round completion. Provider receipts now charge the shared job budget immediately; reaching it stops owned work promptly, including delegated tasks and background commands."
  - "Budget changes previously only allowed increases. The owner can choose 5, 10, 20 or 30 USD in either direction, provided the new cap is not below recorded spending."
---

Manager and stage families share live accounting without charging folded child
usage twice. Accounting failures refuse further work. Already dispatched provider
calls can still deliver charges after cancellation. The legacy raise_budget route
and capability key remain compatible; new history says changed budget.
