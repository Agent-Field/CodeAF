---
kind: fixed
title: Five tests that failed dev's full suite now and then are fixed at the cause, and codeaf's own copies no longer leave git maintenance running
pr: 2548
surface: [build, engine]
invalidates:
  - "A git command codeaf ran inside a copy it owns (a task copy, a review or history scratch repository, a family repository, a merge round, the senior-dev copy, the craft repository) could leave a detached `git maintenance run --auto` writing there after it returned; those commands now run with maintenance.auto=false and gc.auto=0, while commands in the person's own repository keep git's normal behaviour."
  - "TestTheDailyCeilingBindsTaskAndErrandTurnsToo, TestTaskWorkspaceApplyRejectsAlteredStageReceipt, TestSeniorDevShellWorksInAPlainFolder, TestR9R23PersonalLearnerRefreshesColdRouterGeneration and TestANewDirectionArrivesWithTheClaimUnspent each failed a nightly Full check or a Promote to staging now and then; each read state it did not own, raced a background writer, or waited on a clock nobody advanced, and each now owns what it reads."
---
Staging had not moved since the v0.8.2 roll-up on 2026-10-23, because each Friday's
promotion met a different one of these. Test fixture repositories now save
maintenance.auto=false and gc.auto=0 when they are made.
`TestLowCreditReadSwapsOnlyAnUntouchedImplicitConversation` failed once on 2026-10-24
and has not failed since. No cause was found, so it is unchanged.
