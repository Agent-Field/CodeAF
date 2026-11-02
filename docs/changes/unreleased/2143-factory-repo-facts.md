---
kind: added
title: App clients can read one repository's facts counted by the factory
pr: 2143
surface: [engine]
invalidates:
  - "Was true: facts about a repository beyond the queue's recent jobs (monthly spend, merged this month, struggle by area, stuck steps) had to be counted by the app from factory.queue. Is true now: factory.repo_facts reads the factory's GET /v1/repos/{owner}/{repo}/facts, counted over every job document the factory keeps."
---

`factory.repo_facts {factory, repo, days?, months?}` (hello feature
`factory.repo_facts`, capability `repo_facts`) answers
`{factory, repo, as_of, time_zone, days, months, facts, truncated?}`. Each fact
is `{id, kind, value, phrase, about, source}` with `source` beginning
`engine:` and a phrase that never shows a number without its noun: jobs
finished and pull requests merged in the last `days` (1 to 366, default 30)
and in this calendar month, spend for each of the last `months` calendar
months (1 to 12, default 3) by the placement rule of `factory.spend_by_repo`,
jobs, rounds and questions by area by the rule of `factory.job_area`, and open
jobs whose current step began 24 hours or more ago, in all and by step. At
most 20 areas and 20 stuck steps are answered, busiest first (at most 97
facts); `truncated` counts the rest. A repository the caller cannot read is
answered not found. No manual page changes: this is an app wire read with no
chat or resident surface.
