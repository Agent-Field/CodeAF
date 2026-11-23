---
kind: fixed
title: Today's spending moves with every ledger row and names Factory AI
pr: 2275
surface: [engine]
invalidates:
  - "Today's figure could stay stale until a chat turn ended, because spend.changed fired only then."
  - "Factory triage, scouts and page AI read as your chats in today's split."
---

Today's figure on the Spending page moves when any ledger row lands. It used to
move only when a chat turn ended, so Factory triage, scouts and page AI left it
stale until the next chat. `spend.today` and `spend.changed` gain `factory_ai_usd`,
the part of today that triage, scouts and page AI spent, absent when zero. A row
tagged with a job stays with the job.
