---
kind: added
title: A pipeline change can be read from, and added to, the pull request that is already open
pr: 2134
surface: [engine]
invalidates:
  - "factory.pipeline_pr could only open a NEW pull request, and the factory route refused adding a commit to an existing one as an unknown field. With the hello feature `factory.pipeline_pr_update` it takes `pr` (and optionally `expected_head_sha`) and commits the file onto that open pull request's head branch, so the desk no longer opens a second pull request while the first is open."
  - "factory.pipeline_source read only the default branch, so a second change to an open pipeline pull request was built on the default branch's file and dropped the first change. With the hello feature `factory.pipeline_source_ref` it takes `ref` (a pull request number such as \"42\", or its 40-hex head commit) and reads that open pipeline pull request's file at its head; `base_sha` is then that head. The factory's `GET .../pipeline/edit-source` took no query and now takes `?ref=`."
  - "The failure stages were request, credential, repository, permission, base, file, branch, commit and pull_request. A new stage, `head`, names a refusal to write to the pull request asked for: closed, missing, from another repository, into another base, or moved since the draft started."
---

`factory.pipeline_pr {factory, repo, text, title?, pr, expected_head_sha?}`
answers `updated` (`pr.head_sha` is the new commit) or `unchanged` (the head
already holds those bytes; `pr` is the pull request as it stands). `branch`,
`body` and `expected_base_sha` are refused with `pr`. The write is sent once; a
failure never leaves a branch, because the branch was there before the call.
`FactoryCapabilities.pipeline_pr_update` says the engine serves it. An engine
without the feature refuses `pr` as an unknown field.

The head is read from the pull request's branch ref, not from the pull request's
`head.sha`, which GitHub answers a few seconds late after a push; a caller
pinning the commit an update just answered is not refused as moved.
GitHub's contents API guards the file, not the branch: a push to the pull
request after the read that leaves the pipeline file alone is built on. An
engine with the feature attached to an older factory answers `uncertain` for a
request with `pr`; nothing is written. App wire only: no manual page describes
this behaviour yet.

`factory.pipeline_source {factory, repo, ref}` reads the file at an open
pull request's head. The pull request must be open, from a branch of the same
repository into its default branch, and change only the pipeline file;
anything else is `conflict`. A number is resolved to the branch's tip when the
read is made, so a read again after "has new commits" gets the new head. The
answer echoes `ref` and names `pr`; a default-branch read names neither. The
preview takes that head as `base_sha`, and a pipeline that spells `when`,
`effort` or `fanout` is sealed at that head when it is committed with `pr`.
A ref read never compares with or clears the factory's own copy of the file.
