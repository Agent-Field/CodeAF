---
kind: fixed
title: Every step's brief says which paths to ask about and which never to touch
pr: 0
surface: [engine, docs]
invalidates:
  - "Was true: a job's ask-first and never lines (the repository's `start.ask_first` and `start.never` with the factory's own) were checked only when the job started and when its plan was approved; no step was told them, so a rule such as `touch auth/` reached the step that would touch auth/ only as the approval after the plan. Is true now: the rules paragraph of every step's brief adds them after the rule sentences: \"ask before touching: auth/, billing/\" for path rules, \"ask before you: …\" for sentence rules and \"never touch: infra/\" for never lines. A job with none of them says nothing more."
---
