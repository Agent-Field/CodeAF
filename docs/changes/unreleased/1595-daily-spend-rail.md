---
kind: fixed
title: interactive chat turns and task crew delegations enforce the daily spend rail
pr: 1595
surface: [chat, engine]
invalidates:
  - "Interactive chat turns and task crew delegations previously bypassed the project-wide daily spending rail and only checked per-conversation limits. Both paths now enforce the daily spend ceiling configured in the profile. Guards refresh from the shared ledger and reset completed spending at the local date boundary."
---
