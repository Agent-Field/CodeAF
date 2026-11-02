---
kind: fixed
title: Automatic factory approvals and publications no longer credit a person
pr: 2237
surface: [engine, docs]
invalidates:
  - "Automatic approval and publication events credited the job owner or runner as though they had acted. New automatic events identify the factory instead, and stale replies say approved automatically at the approval time."
---

Auto records its own approvals and publications without a person's handle.
A person's approval still names them. Stale replies after automatic approval
say "approved automatically at" and the time in UTC. Existing saved events
keep their original attribution. Automatic publication still requires a
recorded approval bound to the exact commit; it grants no merge authority.
