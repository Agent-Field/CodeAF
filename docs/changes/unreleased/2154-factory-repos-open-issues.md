---
kind: added
title: The factory's repository list says each repository's open issues apart from its pull requests
pr: 2154
surface: [engine]
invalidates:
  - "Was true: a repository row's only count was open, GitHub's open_issues_count, which counts open issues and open pull requests together. Is true now: rows also carry open_issues, open issues alone, from one batched GraphQL request per 100 listed repositories; it is absent when GitHub did not answer it, never 0 in its place."
  - "Was true: the first-run setup's repository rows sent open_issues as GitHub's open_issues_count, issues and pull requests together, so the setup read's progress total overshot what it reads. Is true now: discovery's open_issues counts issues alone, from the same one-GraphQL-request-per-page batch, with open_issues_exact true; a repository GraphQL did not count keeps the combined number with open_issues_exact false, because the field stays required."
---

`factory.repos` `available[]` and the factory's `GET /v1/repos` rows gain
the optional `open_issues`. `open` is unchanged. There is no hello word: the
field's absence already means "not known", so a reader falls back to `open`.

The setup wire's `open_issues` keeps its name and type (a required number);
the new `open_issues_exact` says whether it is issues alone. The setup read's
own count was already issues alone and is now marked exact too.
