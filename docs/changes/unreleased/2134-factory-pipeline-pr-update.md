---
kind: added
title: A pipeline change can be added to the pull request that is already open
pr: 2134
surface: [engine]
invalidates:
  - "factory.pipeline_pr could only open a NEW pull request, and the factory route refused adding a commit to an existing one as an unknown field. With the hello feature `factory.pipeline_pr_update` it takes `pr` (and optionally `expected_head_sha`) and commits the file onto that open pull request's head branch, so the desk no longer opens a second pull request while the first is open."
  - "The failure stages were request, credential, repository, permission, base, file, branch, commit and pull_request. A new stage, `head`, names a refusal to write to the pull request asked for: closed, missing, from another repository, into another base, or moved since the draft started."
---

`factory.pipeline_pr {factory, repo, text, title?, pr, expected_head_sha?}`
answers `updated` (`pr.head_sha` is the new commit) or `unchanged` (the head
already holds those bytes; `pr` is the pull request as it stands). `branch`,
`body` and `expected_base_sha` are refused with `pr`. The write is sent once; a
failure never leaves a branch, because the branch was there before the call.
`FactoryCapabilities.pipeline_pr_update` says the engine serves it. An engine
without the feature refuses `pr` as an unknown field.

GitHub's contents API guards the file, not the branch: a push to the pull
request after the read that leaves the pipeline file alone is built on. An
engine with the feature attached to an older factory answers `uncertain` for a
request with `pr`; nothing is written. App wire only: no manual page describes
this behaviour yet.
