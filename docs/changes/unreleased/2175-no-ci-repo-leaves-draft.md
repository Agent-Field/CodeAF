---
kind: fixed
pr: 2175
title: A repository with no GitHub checks configured is reviewed without CI and its PR leaves draft
surface: [engine, app]
invalidates:
  - "ReadinessGate required at least one passing check, so a repository with no CI kept its factory PR a draft forever. A settled no-checks reading (no .github/workflows file on the PR head and no statuses or check runs, held on that head for factory.NoChecksSettle, 3 minutes) now counts as a stated condition: the readiness review runs and the PR is marked ready. Pending or failing checks behave as before."
  - "PullRequest gains NoWorkflows, NoChecksSince and NoChecks. Source.Follow and the follow poll settle them with factory.SettleNoChecks; a push clears them. The factory API's PR carries them as no_workflows, no_checks_since and no_checks."
  - "The job's words say \"No GitHub checks in this repo; reviewed without CI.\" (factory.NoChecksWords) on the readiness review and on monitoring afterwards."
  - "The merge gate's draft refusal is no longer the bare \"the pull request is a draft\"; it names why the PR is still a draft and what will move it (factory.DraftWhy)."
---
