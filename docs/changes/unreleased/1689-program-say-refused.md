---
kind: fixed
title: a note or say to a running senior-dev task is refused instead of stored where nothing reads it
pr: 1689
surface: [chat]
invalidates:
  - "The chat's `tasks` say to a running senior-dev task answered `Its worker is handed it as soon as the step it is on ends`, but senior-dev's worker never reads notes, so the words sat unread. It now answers `nothing was noted: senior-dev reads no messages` and writes nothing."
---
