---
kind: fixed
title: Jobs without a GitHub issue no longer borrow an issue number
pr: 2239
surface: [engine]
invalidates:
  - "Was true: a job without an issue displayed its local id as a GitHub number, and step briefs called it an issue. Is true now: it displays as job 21 and its steps are told it has no GitHub issue; real issue references stay unchanged."
---

Jobs without a GitHub issue use a local job label. Plan, Review and Proof no
longer point agents at an unrelated issue. Issue comments are refused before
calling GitHub. Saved job identities, links, branches and chat markers stay
compatible, with no migration.
