---
kind: added
title: The engine can open a pull request for a repository's pipeline file as the signed-in caller
pr: 0
surface: [engine]
invalidates:
  - "The caller-scoped `POST /v1/repos/{owner}/{repo}/pipeline/open-pr` was reachable only as factory HTTP; the app wire had no verb, no hello feature and no capability for it, so the desktop could not call it. `factory.pipeline_pr` now carries it, optionally. It is a transport seam: it does not edit a pipeline, build or diff YAML, or read the repository's file."
  - "Nothing on the app wire could say that a pull request MAY have been opened. A reply the engine cannot confirm is now `uncertain`, and a bare 4xx is never read as \"not opened\": the route sends no `X-Codeaf-Phase` marker, so only the flow's own typed failure (outcome, known stage, request id, agreeing status and `branch_left`) is believed."
---

A distinct optional feature, `factory.pipeline_pr`, adds one method,
`factory.pipeline_pr {factory, repo, text, title?, body?, branch?,
expected_base_sha?}`, with the route's own field names and bounds.
`FactoryCapabilities.pipeline_pr` says the engine serves it. Any other field, a
token, a path, a reviewer, is `bad_params`.

It needs a credential held for the connection right now and works for any
signed-in person, not only the operator. The engine sends that credential and
nothing else: it never supplies a GitHub token or reads the host's `gh` or git
identity, and with no credential held it refuses before any request leaves.

One `POST`, never retried or sent to another route. `opened` and `unchanged`
are results. The flow's typed `refused`, `not_opened` and `unknown` are errors
whose `detail` keeps the route's outcome, stage, `github_status`, branch,
`branch_left` and request id, in words fixed by the engine. Everything else (a
lost connection, a redirect, an unexpected status or body, a contradictory
reply, or any reply for a caller who has since been replaced) is `uncertain`
and names no repository, branch or pull request.

This is the engine half only, and no desktop work is in this change. It does
not read the repository's file. `factory.pipeline` already returns the
repository file as read in `pipeline.text` (the personal file is separate, in
`personal_text`), but it carries no commit that text was read at, so
`expected_base_sha` has nothing to be filled from; and `text` is the YAML file
only when `source` is `file` and `origin` is `main`. The review lists these.

`pr: 0` because no pull request is open yet.
