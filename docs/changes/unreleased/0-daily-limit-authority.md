---
kind: fixed
title: codeaf desktop and task guards share the saved daily spending limit
pr: 0
surface: [engine, chat]
invalidates:
  - "Was true: the desktop kept a separate default $20 day cap while task guards read a profile default of $500, and a desktop raise did not reach existing guards. Is true now: one atomic profile authority owns the base limit and expiring today raise. Legacy migration retains the tighter positive saved limit, occurs before engine opening or quit recovery, and cannot undo a later explicit write. Settings, Welcome, spending and task/helper guards read that authority; restored guards refresh it before calls. Environment overrides refuse ineffective desktop writes."
  - "Was true: unreadable daily-limit configuration or ledger reads could grant work as if spending were zero. Is true now: daily admission propagates those failures, preserves pending quit recovery, reads one bounded ledger line at a time, and refuses potentially paid guarded calls. Legacy malformed/oversized rows retain their existing skip behavior, with explicit degraded counts available to readers; no neighboring-row date is inferred and no exact-completeness claim is made. Explicit no-limit profiles may adopt the older app default of $20 as the tighter limit on first app migration. An older binary can retain a newer today raise until midnight. This does not add a universal transport gate or monthly cap: already admitted main turns and native media remain outside that future shared gate."
---
