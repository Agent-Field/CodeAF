---
kind: added
title: codeaf app reports spending by local day, model and subject
pr: 0
surface: [engine]
---

Was true: the only ways to see the usage ledger were `spend.export` (raw
calls) and `spend.ledger` (rows by run); a calendar or model breakdown had to
page every row and add them up itself.

Is true now: `spend.report` takes `spend.ledger`'s filter (both days required,
at most 366 apart) and answers cells — one per stored local day, model and
subject, with a task's conversation — plus exact totals, the distinct models,
and `gaps` (records this process dropped, lines skipped, days derived). The
engine folds nothing. The ledger records no billing provider, task outcome or
crew, so no cell carries one and `not_recorded` says so; an unnamed model has
no model, an unpriced cell no cost. More than 8 192 cells, an estimated 4 MiB,
or a 4 MiB reply is refused as `bad_params`, never cut. It is one linear pass
over the ledger's length at the start, with no cache; the byte figure is an
estimate, not a memory promise. `spend.ledger` is unchanged. Engines without
the capability answer `unknown_method`.
