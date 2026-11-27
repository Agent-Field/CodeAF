---
kind: fixed
title: Open personal apps on populated workspaces
pr: 2998
surface: [app]
invalidates:
  - "A draft used to get a handful of examples per type regardless of its opening queries. The default builder now fills those queries from the named sample story and checks for at least 20 distinct examples."
  - "A record change used to leave manual reference inputs empty. A matching typed input can now take the triggering record while manual commands still supply their own ID."
---

Bare collections gain a record workspace. The builder gives a Kit example for an unsupported run step shape, repairs unambiguous aliases on retry, and names unwritten automations it drops from the plan and triggers.
