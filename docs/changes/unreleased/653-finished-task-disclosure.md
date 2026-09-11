---
kind: fixed
title: Finished task scrolling preserves collapsed work and disclosure arrows match their state
pr: 653
surface: [chat]
invalidates:
  - "Scrolling above the top of a completed task could open its work and then every tool step. Finished-task scrolling now moves only the reading position; click or Ctrl+E opens the work."
  - "The worked chip always displayed a closed arrow, even above an expanded outline. Its arrow now reflects the effective disclosure state, including ui.work = open."
---

Completed task rooms keep the request and final reply visible with intermediate
work behind one disclosure. Repeated wheel input at the top no longer opens that
disclosure or its tool steps. Explicitly opening work reveals the step outline;
opening a step reveals its calls. Closing the outer disclosure hides all of them.

The running room's existing scroll-to-earlier-calls behavior remains. The explicit
`ui.work = open` preference still starts work expanded, and returning to a completed
task retains disclosures the reader deliberately opened. No journal data changes.
