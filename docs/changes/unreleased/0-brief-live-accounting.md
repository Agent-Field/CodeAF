---
kind: added
title: Live coverage for job brief spending
pr: 0
surface: [engine, app]
invalidates: []
---

An opt-in live test checks that a real job and its brief charge the same amount
in the job header, Spending ledger and provider records. Memory is disabled for
this check so its separate charges cannot obscure brief accounting. Every role
uses the same model with the Model Pool off; the run stops at the approval gate.
