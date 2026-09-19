---
kind: fixed
title: a hosted conversation reads and steers its run
pr: 1230
surface: [chat, engine, remote]
invalidates:
  - "A run's rows, its tasks' pages, the six steering verbs and the run summary existed only in a conversation whose engine ran in the same process (`codeaf chat --no-host`) and in tests. `codeaf chat` hosts its engine by default, and there the rail drew one row with the task's internal number and nothing under it. All ten methods now cross the wire, and `remote` holds a compile-time assertion that its client is a `session.PlanAgent`, so a method added to that set fails the build until it is carried."
  - "The rail re-read a run's plan only when the reading of other windows' work took a new stamp. A hosted conversation has no such reading, so its rail stood on the run's first row until the run ended. The plan has a three-second beat of its own."
  - "The plan reads and verbs were called on the update loop. They go through the ordered door line, the page's follow read is one at a time, and an answer is folded only into the page that asked."
---

Seen on the real binary on 2026-09-19. The same two-part task was handed off
hosted and unhosted side by side: unhosted the rail drew each part ten
seconds after the run added it, hosted it drew them only at the end. After
this change the hosted rail drew the parts at ten seconds and each check as it
was added, a part's page opened with its description and steps, and a note
typed on that page was on the page a moment later.

The refresh of a run's summary carries how long the caller will wait and never
the instant it stops waiting, because an engine reached with `--host` runs
under another machine's clock.
