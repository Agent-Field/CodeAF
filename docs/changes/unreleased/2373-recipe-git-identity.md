---
kind: fixed
title: Banking a recipe works on a machine with no git identity
pr: 2373
surface: [factory]
invalidates:
  - "Banking a recipe used the person's git identity and failed with \"Author identity unknown\" when none was set. The recipe commit is now made as codeaf, like the other commits codeaf makes."
---

Banking a recipe in a Git repository now commits the change as `codeaf <agentfield-bot@users.noreply.github.com>`. You no longer need a git identity configured for it to succeed.
