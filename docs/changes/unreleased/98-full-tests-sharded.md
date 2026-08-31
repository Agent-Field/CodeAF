---
kind: changed
title: the full suite runs as three shards, and known-red now carries what Linux found
pr: 98
surface: [build]
invalidates:
  - "The full suite ran as one job and its first run was killed under the link load of the heaviest test binaries. It runs as three round-robin shards now, and a hang names its package via `-timeout 8m` instead of dying silently."
  - "The four `cmd/harness-design` known-red tests were unnamed everywhere. They are named in `.github/known-red.txt`: TestTheGuidesRender, TestReviewerCarriesTheWholeLaw, TestBothBriefsSayTheSyntaxIsASCII, TestTheReviewerBriefCarriesBothDuties."
  - "`internal/remote TestTheEnginesStandingStoreAnswersOverTheWire` was believed green — it is red on macOS and Linux both, is now in the ledger, and needs an owner."
---
