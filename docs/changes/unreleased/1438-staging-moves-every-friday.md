---
kind: added
title: staging moves itself every Friday after the full check, and stageaf installs it beside codeaf
pr: 1438
surface: [chat, build, docs]
invalidates:
  - "staging moved only when a person ran `git push origin <sha>:staging`. `Promote to staging` (.github/workflows/promote-staging.yml) now moves it every Friday to the newest first-parent dev commit whose committer time is at or before 17:00 America/Toronto, and only after a fresh Full check on that exact commit passes. It pushes with the PROMOTION_TOKEN secret and posts every outcome to SLACK_RELEASE_WEBHOOK; without the token it reports and moves nothing."
  - "docs/rules/promotion.md step 3 ran `gh workflow run ci-full.yml --ref $SHA`, which GitHub refuses for a sha. The Full check now takes a commit: `gh workflow run ci-full.yml --ref dev -f ref=$SHA`, and another workflow can call it with `ref`."
  - "devaf was the only side-by-side install. `curl -fsSL https://agentfield.ai/get/stageaf | bash` installs the staging build as `stageaf` beside codeaf, and a stageaf's own reinstall line names that address."
  - "Nothing said when staging was ready for main. The Friday run posts the staging commit from before its own move as the production candidate, with the exact fast-forward to run; main stays a person's push and runs one week behind staging."
---

The cutoff is a commit time rather than the run time because this repository's
scheduled runs start hours late. `workflow_dispatch` retries a failed Friday
(`target` empty for the dev tip, `cutoff` for the Friday rule, or a dev sha) and
`dry_run=true` exercises the check and the messages without pushing.
