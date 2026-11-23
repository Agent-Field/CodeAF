---
kind: fixed
title: Factory publish opens the PR when the branch already exists on GitHub
pr: 2201
surface: [engine, app, docs]
invalidates:
  - "A refused push failed the publish with push_rejected, and Retry publish could not succeed. A refused push now goes to a spare branch name or replaces the job's own last push; push_rejected remains only for a repair whose PR branch moved."
  - "A worker's failed publish left publish_error empty on the factory. It now holds a plain reason."
---

The factory never overwrites a tip it cannot show this checkout pushed: a replacement
carries a lease on that commit, and any other tip stays as it is while the approved
commit goes up as `<branch>-2` through `-9`. The new code `branch_names_taken` says when
no name is free. `pr.head_branch` names the branch a repair pushes to. A re-run from a new
home opens a second draft PR beside the older one, which stays open for the person to close.
