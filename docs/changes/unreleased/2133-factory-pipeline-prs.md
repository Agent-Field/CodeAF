---
kind: added
title: App clients can list a repository's open pull requests that change its pipeline file
pr: 2133
surface: [engine]
invalidates:
  - "Was true: an app knew of an open pipeline pull request only when it had opened that pull request itself in the same window. Is true now: factory.pipeline_prs lists the open pull requests into the default branch that change .codeaf/factory.yaml, from any author, read with the caller's own GitHub credential."
---

`factory.pipeline_prs {factory, repo}` (hello feature `factory.pipeline_prs`,
capability `pipeline_prs`) asks the factory's new read-only
`GET /v1/repos/{owner}/{repo}/pipeline/prs`. One read costs at most 22 GitHub
requests: the repository, one page of 100 open pull requests, and the changed
files of at most 20 of them, the factory's own `codeaf/pipeline-` branches
first. `complete` is false when that bound may have hidden one, and each pull
request says `factory_branch` when its branch is named the way the factory
names its own. No GitHub credential is `github_expired`.
