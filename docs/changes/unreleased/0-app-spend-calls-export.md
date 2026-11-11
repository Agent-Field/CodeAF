---
kind: added
title: codeaf app saves every matching spending call as a twelve-column CSV
pr: 0
surface: [engine]
---

Was true: the only per-call file was `spend.export` (the app's own folder, no
filter) and `spend.ledger.export` saved run-grain rows with its own 23 columns;
nothing saved the filtered calls in the Spending page's column order.

Is true now: `spend.calls.export` takes `spend.ledger.export`'s params and
writes one row per ledger record with `date,time,crew,agent,task,model,
provider,outcome,amount_usd,requests,tokens_in,tokens_out`. `agent` is the
recorded root conversation, else the session, so a chat and its tasks both
match an agent filter. A record covering many requests stays one row with
those requests; an unpriced call has a blank amount. The ledger records no
crew, provider or outcome, so those three columns are blank on every row and
`not_recorded` says so. The answer has no run count: the pass keeps nothing
but the line it is on. Same whole-file, no-overwrite writer as the ledger
export. `spend.ledger.export` is unchanged; engines without the capability
answer `unknown_method`.
