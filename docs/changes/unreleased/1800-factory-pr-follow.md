---
kind: added
title: The factory follows pull requests it opened until they merge or close
pr: 1800
surface: [chat, engine, docs]
invalidates:
  - "A job was done when its pull request opened, and the factory never read merge, close, reviews or later checks. The job stays done; pr.state is open, merged or closed, and each poll re-reads pull requests the factory opened."
  - "The queue summary did not count merges. since.merged_today and since.merged_week count pull requests merged today and this week."
  - "The record had no merge or close. A poll now appends pr_merged or pr_closed the first time it sees that state."
---

PR open and Merged stay in the design. They are `pr.state` on a done job, not a new job state. Contract stays 1.
