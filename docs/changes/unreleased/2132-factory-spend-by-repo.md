---
kind: added
title: App clients can read each repository's spend over a range of the factory's days
pr: 2132
surface: [engine]
invalidates:
  - "Was true: no read split spend by repository; spend.report has no repository and covers only this engine's own calls. Is true now: factory.spend_by_repo reads the factory's GET /v1/spend/repos, which adds up every job's spend by repository from the job documents the factory keeps."
---

`factory.spend_by_repo {factory, from, to}` (hello feature
`factory.spend_by_repo`, capability `spend_by_repo`) answers
`{factory, from, to, time_zone, repos: [{repo, spent_usd, jobs}], total_usd}`
for the repositories the caller can read. Days are the factory's own calendar
days, both inclusive, at most 366. `time_zone` is the IANA name of that zone
(such as `Europe/London`); a factory on its process's local zone names it from
`TZ` or the `/etc/localtime` link, and leaves the field off when neither names
one. Each job's whole spend is placed on the day its
run started (a job that never ran: the day it was created), so a run that
crosses a month counts in the month it started, and a job that ran again
counts only its latest run. No manual page changes: this is an app wire read
with no chat or resident surface.
