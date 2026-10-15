---
kind: internal
title: the held-send and plan tests pass under -race; their fakes now lock what off-loop readers touch
pr: 1836
surface: [chat]
invalidates:
  - >-
    The tui3 held-send tests (TestAFailedHeldSendStays…,
    TestAWaitingPictureSends…, TestComingBackDuringAHeldSend…,
    TestHeldWaitingMessagesGoOnePerLanding…) and
    TestStopOnTheRunsOwnPageRaisesTheCardAndTheCardStopsTheRun were believed to
    fail under -race because the keeper goroutine or the room touched state the
    update loop owns. They did not: the races were in the test fakes. switchAgent
    read and wrote `running` without a lock while a kept conversation's watcher
    called Attach from its own goroutine, and planFake handed the surface its own
    rows slice and then rewrote it from a plan verb running off the loop. The
    fakes now lock those fields, PlanTasks answers a copy as the store does, and
    a test that changes them while a watcher or read is live goes through
    switchAgent.setRunning or planFake.setPage.
---

No product behavior changed. The real agents already take their locks in Attach,
PlanTasks and PlanTaskPage, and the real PlanTasks builds a fresh slice on every
call. Three more tests with the same fake races, also red under -race on dev,
are fixed by the same change: TestAProgramRoomReadsTheLandingTheStoreEndedAhead,
TestTheCancelKeyOnAPlanRowEndsItThroughTheStore and
TestPOnAPlanRowPausesThenResumes.
