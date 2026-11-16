---
kind: fixed
title: A person's Stop no longer reads as a failed step, and santosh/work's six red tests are green
pr: 2699
surface: [engine, factory]
invalidates:
  - "Was true: a factory step whose worker heard the person's Stop through its lease (factory worker: stopped by person) before the runner's loop was cancelled was stamped as a step failure, and the job's manager was asked, and paid, to write a one-line summary of that failure, in a conversation not booked on the job. On a busy machine this happened about half the time. Is true now: a lease the factory took back ends the runner's loop without a failure and without a summary turn, as the boundary check already did."
  - "Was true: TestHostedTakeBackReachesAcquireWithoutSharedUploadsOrReleaseDelay held the take-back to one second of wall clock before its first acquire. Is true now: it bounds the time that no measured phase accounts for, and each handoff phase (retirement, directory, acquire), so a slow git read on a loaded box does not count as a release delay."
  - "Was true: TestTheFixedPrefixStaysUnderItsBudget had been red on dev since the app/v3 landing (#2561), 13 bytes over. Is true now: the deliverables line in system.md is shorter and the prefix is under its unchanged budget."
---

TestC7R2ReopeningChatStopsPreviousDrive's fixture displaced a drive by
creating its record under the same computer. Since #2654 that is the copier
sharing a new chat, which the drive rightly keeps, so the fixture now has
another computer hold the record. TestAClosedNodesDollarsAreStampedOnTheConversation
now settles the deferred meta.json stamp before it reads it. The workstream
tools decode their arguments through decodeToolArguments. CODEAF_CANVAS_BIN
(#2638) and CODEAF_FACTORY_RUNTIME_DIR (#2609) are registered as operator
plumbing, so TestRegistryCoversEveryUserFacingEnvironmentPin is green again.
