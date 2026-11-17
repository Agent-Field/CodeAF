---
kind: changed
title: Pull requests into app/v3 run the light merge gate
pr: 2430
surface: [build, docs]
invalidates:
  - "Was true: PRs into app/v3 ran only CLA. Is true now: PRs into app/v3 run the same light merge gate as dev, including build, vet, formatting, packed manuals, change entry, manual law, structural laws and touched-package checks, comparing against the PR's app/v3 base SHA."
---

The existing check aggregate requires both the light gate and touched-package
checks to succeed. Package legs run only when Go or module files change, just
as they do for dev PRs. Existing dev triggers and gate jobs are unchanged.
