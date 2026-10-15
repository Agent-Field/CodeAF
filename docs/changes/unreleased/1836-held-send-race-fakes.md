---
kind: internal
title: the held-send and plan tests pass under -race; their fakes now lock what off-loop readers touch
pr: 1836
invalidates:
  - >-
    The tui3 held-send and run-stop-card tests were believed to fail under -race
    because the keeper goroutine or the room touched state the update loop owns;
    the races were in the switchAgent and planFake test fakes, which now lock
    what the off-loop readers touch, and production code is unchanged.
---

The real agents already lock Attach, and the real plan reads build fresh values
on every call. Three more tests with the same fake races, also red under -race
on dev, are fixed by the same change: TestAProgramRoomReadsTheLandingTheStoreEndedAhead,
TestTheCancelKeyOnAPlanRowEndsItThroughTheStore and TestPOnAPlanRowPausesThenResumes.
