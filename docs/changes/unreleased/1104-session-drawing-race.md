---
kind: internal
title: The carry-ladder session tests wait for the mark's drawing instead of racing it
pr: 1104
surface: [engine]
invalidates:
  - "`TestASuccessfulHandoffJournalsTheRungThatSuppliedTheBrief` could fail inside a full `make check` with the draft as the brief. The checkpoint fixtures now wait for the drawing beside the turn to land, and the race has a regression test."
  - "The race looked like an errand stealing a scripted step. It is not: the drawing is a sidecar, so the round count before it lands drifts with the scheduler and the writer's ask could land past the end of the script."
---
