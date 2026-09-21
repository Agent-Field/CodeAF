---
kind: fixed
title: the drafted-with footer links to the codeaf repository
pr: 1329
surface: [engine, docs]
invalidates:
  - "The three attribution footers linked to `https://agentfield.ai/github`, which 302s to `github.com/Agent-Field/agentfield`. They link to `https://agentfield.ai/github/codeaf`, which 302s to `github.com/Agent-Field/CodeAF`. The utm parameters and the rest of each line are unchanged."
---

Every pull request, issue and comment codeaf drafted ended with a line
offering to show the reader what drafted it, and the address under that offer
went to a different repository. The redirect for this one already serves, so
the fix is the longer path and nothing on the site has to move.

`AttributionPullFooter`, `AttributionIssueFooter` and `AttributionCommentFooter`
in `internal/exec/linear.go` carry the new address, with the fixtures that pin
them and the two manual pages that quote them. The CHANGELOG keeps the old
address wherever it records what the line used to be.
