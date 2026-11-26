---
kind: fixed
title: App builds recover saved work and explain changes in plain words
pr: 2974
surface: [app, engine]
invalidates:
  - A builder pass that reached its call limit stopped with unfinished work. The builder now saves progress and can continue automatically up to twice.
  - The builder could repeat its technical reply in the change card. The card now shows the part and its before and after states while technical details stay in Build activity.
---

A missing plan gets one correction attempt. Checks catch unresolved draft
placeholders and record actions placed away from their records. Metrics use
the declared query, and run approvals identify the records by title.
