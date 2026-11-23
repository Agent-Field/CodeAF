---
kind: changed
title: A new chat in codeaf app starts without rereading the whole spend ledger or the skill folders
pr: 2098
surface: [engine]
invalidates:
  - "Was true: session.ReadDailySpend (and DailySpendUSD) walked the whole usage ledger on every call, and a first turn called it three times. Is true now: it keeps a per-ledger, per-day tally and reads only what was appended since; a replaced, shorter or rewritten ledger, or a new day, is read from its start."
  - "Was true: the first conversation a codeaf app process opened waited for the installed-skills scan. Is true now: the hello's warm (appBackend.Warm) reads the skill shelf for the work folder a new chat opens in, and primes the day's spend, before any agents.create."
---
