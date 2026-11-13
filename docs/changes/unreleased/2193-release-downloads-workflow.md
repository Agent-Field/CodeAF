---
kind: removed
title: The daily Release downloads workflow and its PostHog script are gone
pr: 2193
surface: [build, docs]
invalidates:
  - "A daily workflow, .github/workflows/release-downloads.yml, sent GitHub release download counts to PostHog as codeaf:release_downloads; that workflow and scripts/release_downloads.py no longer exist and nothing reports download counts."
---

It failed on every run because its PostHog project key was never set, and the
counts are already visible on the GitHub release pages.
