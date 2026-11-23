---
kind: changed
title: The pipeline pull request opens as the signed-in person over GitHub's REST API
pr: 0
surface: [engine]
invalidates:
  - "POST /v1/repos/{owner}/{repo}/pipeline/open-pr was believed to be safe: it needed the operator, pushed and opened the pull request with the HOST's ambient git and gh identity from a local checkout, and answered 200 with a prose note on failure. It now uses only the caller's own GitHub credential (a team never uses the host or its read grant; a solo host may use its own profile token), needs no checkout, runs no git, gh or other program, and the operator is no longer required. GitHub's own write permission and repository visibility decide who may."
  - "A failed open used to read as success (\"written\", \"branch pushed\", \"committed\"). Success is now outcome \"opened\" with a positive pull request number and the canonical https://github.com/<owner>/<repo>/pull/<n> URL, or outcome \"unchanged\" when the default branch already holds these bytes. Flow failures are non-2xx with outcome refused, not_opened or unknown, plus stage, github_status, branch and branch_left. Shared authentication, visibility and JSON checks retain the ordinary API error shape."
  - "The response is not a transaction and this slice does NOT clean up: it never deletes a remote branch. A branch it created stays on GitHub and is reported with branch_left when a later step fails, because an unknown pull request result or a concurrent push must not lose work. A server restart mid-flight can also leave a branch behind, and nothing records that. A write with an unknown result is never retried."
  - "Not yet: requesting reviewers (a reviewers field is refused as unknown), adding a commit to an existing pull request, monorepo sub-roots, and an activity record of the pull request. The chat's teach-a-line path (BankRecipeChange) is unchanged and still uses the host's git and gh."
---

The route reads the default branch tip and pins it as a commit SHA, reads
`.codeaf/factory.yaml` at that commit, and creates a NEW branch at the same SHA
(the caller's `branch`, or a content-derived `codeaf/pipeline-` suggestion). It never writes
the default branch or an existing one, never forces, and refuses an existing
branch instead of renaming it. `expected_base_sha` (a commit) refuses with 409,
writing nothing, when the default branch has moved. Requests are bounded, time
out together, follow no redirects, and carry the token only in the
Authorization header; GitHub's error text is never echoed.
