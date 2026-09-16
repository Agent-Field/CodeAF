---
kind: changed
title: The workspace-foundation constraints no longer link a private conversation or a private repository
pr: 1064
surface: [docs]
invalidates:
  - "`docs/design/workspace-foundation/CONSTRAINTS.md` linked the chatgpt.com conversation *Math Frameworks for Org Design* and the personal `org-design` ideation repository under its earlier references. Both are named in prose now and neither is linked: the conversation is private to its owner's account and the repository is private, so the links opened for nobody else and a public repository should not carry addresses into private accounts."
  - "The 2026-09-16 pre-flip audit found no live credential anywhere in the repository's history, PR heads, tags, issues or comments. The two remaining history-only items — the retired Google OAuth desktop client from August, and the swe-pro-go source vendored under `internal/swepro` from 2026-08-09 to 2026-09-01 — are owner decisions and are not addressed by a change entry."
---

Before the repository goes public, every remote tag under `checkpoint/`,
`archive/` and `salvage/` (163) and every remote branch whose pull request was
closed or merged (17) were deleted from origin; the tag objects not on `dev`
are kept in a bundle in the owner's handoff folder. Branches with an open pull
request and branches that were never in one were left alone. None of that is
a change to the tree, which is why this entry carries only the document edit.
