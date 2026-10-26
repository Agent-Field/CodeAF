---
kind: added
title: A plan that touches a path a rule guards waits for a person, even in Auto
pr: 0
surface: [engine]
invalidates:
  - "A repository's `start.ask_first` and `start.never` were read only before a job started on its own; a job a person started, and every plan, ran past them. A job now carries the ask-first and never lines it started under (`start_rules` on the wire, the repository's with the factory's own), and when its finished plan names a file one of them covers, the approval after the plan records the rule and file (`rule_hold`) and Auto will not pass it (`autonomy_why` says \"A rule asks first: …\")."
  - "A plan check reads a pattern that ends in a slash as everything under that folder: `touch auth/` covers `internal/auth/login.go`. The start-time match of an issue's words is unchanged."
---

The hold is cleared when the person answers the approval, and when the job is
started again or brought back.
