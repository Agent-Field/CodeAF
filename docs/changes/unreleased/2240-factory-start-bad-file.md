---
kind: fixed
title: codeaf starts jobs safely when repository rules cannot be read
pr: 2240
surface: [engine]
invalidates:
  - "Was true: Start on a new job without a budget failed with the raw repository YAML error and an uncertain outcome, while a chat start could leave it without a budget. Is true now: both starts use the default job budget when the repository file is unread, record a safe explanation, and hold the plan for a person."
---

A bad `.codeaf/factory.yaml` no longer makes Start say the action may have
happened. The job starts with the factory's rules, a bounded budget and an
explanation naming the file and the parser's line when available. Its plan
waits for you, including in Auto. Unread factory rules still prevent a start;
a clean read without a positive default budget still refuses an uncapped job.

The plan approval question now shows that explanation before you approve.
Job reads also name the repository-relative file separately and include the
parser's line only when known. No build step starts before your approval.
