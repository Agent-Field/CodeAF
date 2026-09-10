---
kind: fixed
title: an adaptive run prices a bare model id as unpriced, never at a vendor row whose name merely matches
pr: 752
surface: [engine]
invalidates:
  - "`internal/orchestrate`'s `PriceOf` matched a bare model name against the vendor-qualified row it looked like it belonged to, so a bare `deepseek-v4-flash` was metered at `deepseek/deepseek-v4-flash`'s $0.14/$0.28. A bare id matches no row at all now and falls to `unpriced` ($1.25/$10.00) like any other model the table has never heard of; a qualified id, with or without a `:free` or `@2026-01` suffix, prices from its own row exactly as before."
  - "The fuel table's fallback was reachable only by an id that looked unfamiliar. A direct vendor's base serves bare ids, so on that path the fallback is now the ordinary answer rather than the exception — a run against a vendor's own endpoint meters at $1.25/$10.00 per million until somebody prices it, which is deliberately dear so the 80% mark and the gate still arrive."
  - "`internal/orchestrate`'s `TestMeter` asserted `a bare model name matches its vendor's row`. It asserts the opposite now, beside `TestABareNameIsNotAVendorsRow`, which is table-driven over `prices` itself so a row added later is covered without being restated."
---

No vendor in the current survey returns a dollar figure on `usage`, so every call
made against a vendor's own base falls to this table — and the table answered with
another catalog's price for a different serving of the same name. That is worse
than no price: the tank drained at roughly a ninth of the rate the table's own
comment says an unknown model must drain at, so the warning and the gate that put
the decision in front of a person arrived nine times too late.

No rate was added. Every vendor's model line has rolled over since the table was
written, and a guessed row would be the same defect with a different number.
