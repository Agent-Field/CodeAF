---
kind: fixed
title: Follow PR considers comments without manufacturing changes
pr: 2256
surface: [engine]
invalidates:
  - "No-change feedback reports with missing checklist fields previously ended a paid round before receiving a correction; they now receive the correction during the report call."
---

Follow PR can finish a current-head review of a comment that requests no edit
without making a new diff. Missing review checklist fields now reach the model
when it reports, allowing correction in the same conversation instead of spending
another round to discover the omission. Merge still requires its existing gates.

Trying the same refused no-change review again with unchanged PR observations
now returns a recovery explanation without starting a paid round. New pushes,
comments or check results permit another attempt. Stop remains available, and
attempt exhaustion never bypasses Merge's gates.
A checkout refusal is reread locally before another model call: unchanged files
spend nothing, while restoring the published checkout permits another assessment.
