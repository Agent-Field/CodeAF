---
kind: fixed
title: Old Factory jobs fill missing author access when read
pr: 2392
surface: [factory, docs]
invalidates:
  - "Old outside jobs could keep a blank author tier after an unchanged poll. Reading them now checks the author's repository access and saves a successful result."
---

The check uses the factory's read credential. A forbidden or failed lookup
keeps access unknown, leaves writes refused and shows a reason with a retry
time. A missing collaborator resolves as someone without write access.
Already recorded tiers are preserved by this read-time check.
Jobs without an author or repository skip the lookup. A lookup failure shows
only on the job as read; it never changes saved failures or reconciliation.

Retries wait 1, 2, 4, 8 and then 10 minutes, or longer when GitHub asks.
Concurrent reads share a lookup. Retry state stays in memory; only the existing
author tier is saved, without changing the job's age or saved format.
