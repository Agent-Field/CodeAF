---
kind: added
title: The factory wire returns the pipeline file, today's spend and a join preview
pr: 1800
surface: [chat, remote, docs]
invalidates:
  - "The pipeline route did not return the raw file, start, size, the until sentence, or whether the file was the repository's main copy or a personal file. It does now."
  - "Dry run matched owner/name against the job repo string, so a GitHub job stored as the short name never appeared, and a waiting job had no reason. It matches the short name and the owner, and would_wait names the reason."
  - "since.shipped, asked, handled and spent were filled only on the fixture. A real floor fills them, and spent_today is the runner's figure for today."
  - "There was no pre-auth GET /v1/about. There is one, and every error body carries request_id."
---

`GET /v1/repos/{owner}/{repo}/pipeline` returns the repository text, a personal
file when one tightened it, start, size, origin, and each step's until sentence.
`until` stays the runner's word. Dry run matches a short repository name to its
owner and lists why a backlog job is waiting. A real floor fills the shift, and
`spent_today` follows the runner's day figure. `GET /v1/about` is the join preview
before a token. `GET /v1/me` and `GET /v1/about` report access, poll interval and
who is serving. Every error body carries `request_id`. Taking a job records the
machine name when that machine has registered.
