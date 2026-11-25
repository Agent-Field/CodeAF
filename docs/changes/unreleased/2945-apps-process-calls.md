---
kind: added
title: Fluid apps can show record processes and move a batch of records together
pr: 2945
surface: [engine]
invalidates:
  - "Was true: a checklist guard failed registration and the desk had to assemble a record's process from separate queries. Is true now: checklist_done() and checklist_done(id) read declared progress, data.checklist_set updates a step, and data.process returns state, guard results and blocked reasons, open work, timers, sign-offs and activity at one revision."
  - "Was true: the data door could move only one record at a time, and keeping or deleting an app did not update an already open data store. Is true now: data.transition_batch checks every selected record before committing any move, work_items lists or changes the person's and agents' assignments, and publishing, reverting or deleting a definition updates its data grants."
---
