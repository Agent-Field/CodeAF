---
kind: fixed
title: codeaf keeps plan approval visible at later gates
pr: 0
surface: [engine]
invalidates:
  - "A later approval gate erased the earlier settled plan decision and who approved it. The job now keeps the latest settled decision visible until another approval begins, while each later gate still requires its own consent."
---

Your plan approval stays on the job through later steps, waiting gates and
reopening the app. A later approval has its own undo window and replaces the
single approval summary; Activity keeps the earlier receipts. Wire fields are
unchanged.
