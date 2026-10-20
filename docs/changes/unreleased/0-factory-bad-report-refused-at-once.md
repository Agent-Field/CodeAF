---
kind: changed
title: A factory step's self-contradictory report is refused when it is made
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: `stage_result` held every report, and a review that said done with findings still open, or a proof claim marked ok with blank evidence, was found wanting only by the judge after the round, which spent the round. Is true now: such a report is answered at once with `not reported: you reported 2 findings still open: fix them and report again, or report done: false`, `not reported: claim 2 has no evidence: add what shows it, or set ok: false`, or `not reported: you reported done with no claims: add each claim with what shows it, or report done: false`; it is not held, the report before it stands, and the step can fix it and call again in the same turn."
  - "Still true: an honest shortfall (`done: false`, or a claim marked `ok: false`) is held like any report and judged into another round. The check is the judge's own rules (`factory.ReportFault` beside `factory.Met`), not a second copy."
---

A step whose report gave away its own failure used to learn it a round later.
It now hears the fix on the call, and puts it right before it stops.
