---
kind: fixed
title: a gated task inherits every prerequisite report, bounded and counted, and never cuts its own brief
pr: 476
surface: [engine, chat, docs]
invalidates:
  - "A task gated on many prerequisites inherited every report in full, with no bound, so its own brief could be crowded out of the window. It no longer does: `TaskGraph.inheritedLocked` gives the reports what remains of `taskShapeBriefLimit` (6000) after the task's own brief, shared equally with unused remainder handed to whoever is still clipped, and THE TASK'S OWN BRIEF IS NEVER CUT."
  - "A 4 KiB pot once fed a sink four of six sections and it wrote a confident four-row table. No prerequisite is ever dropped now; when N times `inheritedReportFloor` (512) will not fit, the bound yields and each report still gets the floor. The heading says how many reports there are: `What the work before you learned — N reports:`."
  - "The heading under which a dependent reads earlier work was `What the work before you learned:` with the reports uncut. It is `What the work before you learned — N reports:`, a clipped report is marked with `…`, and a report that fits is handed over unchanged."
  - "`PERF.md` documented no brief cap on what a gated node inherits. It now records `taskShapeBriefLimit` on the reports, `inheritedReportFloor` as the per-report floor, and that the count is printed on the heading."
---

The first bound on this pot dropped the tail silently. This one never shortens
the list: the brief gets thinner, the worker is told how many reports there are,
and a second fit on the division road (`familyOf`) moves a trailing `…` rather
than stacking one.
