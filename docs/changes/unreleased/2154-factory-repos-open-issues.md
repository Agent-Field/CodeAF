---
kind: added
title: The factory's repository list says each repository's open issues apart from its pull requests
pr: 2154
surface: [engine]
invalidates:
  - "Was true: a repository row's only count was open, GitHub's open_issues_count, which counts open issues and open pull requests together. Is true now: rows also carry open_issues, open issues alone, from one batched GraphQL request per 100 listed repositories; it is absent when GitHub did not answer it, never 0 in its place."
---

`factory.repos` `available[]` and the factory's `GET /v1/repos` rows gain
the optional `open_issues`. `open` is unchanged. There is no hello word: the
field's absence already means "not known", so a reader falls back to `open`.
