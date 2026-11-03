---
kind: added
title: Workstream agents wake for morning slots and member job events with undoable changes
pr: 2319
surface: [app, engine]
invalidates: []
---

Workstreams can produce one costed, ranked batch per morning slot or delivered
member-job event. Propose only keeps suggestions; Small changes can edit the
plan and agent pages, with Apply, Undo applied and Not now for each card.
Wakes never start Factory jobs or write the person's pages. Recorded wake costs
count toward workstream spend. Daily caps bound background calls.

The additive wire contract is v0.3, with four wake methods and a wakes change
part. Storage migrates additively from schema 3 to 4 on the first write.
Related-issue wakes are not built. The job event hook awaits the Factory event
source; queue reads do not trigger it.
