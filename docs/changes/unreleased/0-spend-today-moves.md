---
kind: fixed
title: codeaf collects Factory charges after failed and unmetered calls
pr: 0
surface: [engine]
invalidates:
  - "A Factory stream's interim usage could prevent its final receipt from reaching the ledger and job total."
  - "Successful Factory calls without usage could disappear from Spending, and queue overflow could discard charge rows."
---

Today's figure on the Spending page moves when any ledger row lands. It used to
move only when a chat turn ended, so Factory triage, scouts and page AI left it
stale until the next chat. `spend.today` and `spend.changed` gain `factory_ai_usd`,
the part of today that triage, scouts and page AI spent, absent when zero. A row
tagged with a job stays with the job.
