---
kind: added
title: A routing rule's yes/no question is answered by a cheap model, read strictly, and charged as factory filter
pr: 2479
surface: [factory]
invalidates:
  - "route.Judge had no implementation; internal/factory/route/judge is the model-backed one, and judge/judgetest is the fake other packages test with"
  - "the factory's own AI on Spending was triage, scout, the page AI and briefs; an AI filter's question is now also there, as factory filter"
---

A model's reply that is not exactly {"yes":bool,"why":"…"} is an error and reads
as no, so prose, a broken answer, a timeout (20 seconds a try, one retry on a
transient failure) or a missing model never starts work.
