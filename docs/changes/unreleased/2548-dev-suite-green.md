---
kind: fixed
title: Five tests that sometimes failed dev's full suite are fixed at the cause
pr: 2548
surface: [build, engine]
invalidates:
  - "A commit, merge, rebase or fetch codeaf ran inside a copy it owns (a task copy, a family repository, a merge round, the senior-dev copy, the craft repository) could leave a detached `git maintenance run --auto` writing there after it returned; those commands now run with maintenance.auto=false and gc.auto=0. Commands in the person's own repository keep git's normal behaviour, and so does LandRunTree, whose callers include runs in the person's folder."
  - "TestTheDailyCeilingBindsTaskAndErrandTurnsToo, TestTaskWorkspaceApplyRejectsAlteredStageReceipt, TestSeniorDevShellWorksInAPlainFolder, TestR9R23PersonalLearnerRefreshesColdRouterGeneration and TestANewDirectionArrivesWithTheClaimUnspent each failed a nightly Full check or a Promote to staging now and then; each read state it did not own, raced a background writer, or waited on a clock nobody advanced, and each now owns what it reads."
---
Staging had not moved since the v0.8.2 roll-up on 2026-10-23, because each Friday's
promotion met a different one of these. The session, command, taskcopy and
senior-dev fixture helpers this change touches save maintenance.auto=false and
gc.auto=0 in each repository they make.
`TestLowCreditReadSwapsOnlyAnUntouchedImplicitConversation` failed once on 2026-10-24
and has not failed since. No cause was found, so it is unchanged.
