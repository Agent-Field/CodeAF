---
kind: changed
title: the Apps Builder builds through task verbs and takes a second pass instead of failing
pr: 0
surface: [engine, docs]
invalidates:
  - "The Apps Builder had one way to build, a single write with repair rounds. Its default road is now rescue: the same first write, then a bounded second pass of 60 steps, 6 minutes and $0.30 before any failed card."
  - "Only the Builder itself could add pieces to an app draft. A task worker now builds through `codeaf app <verb>` (name, plan, record, field, screen, run, agent, check, try, done), and CODEAF_APPS_BUILDER_ROAD picks calls, loop or rescue."
  - "The plan card appeared whole when the plan call returned. On the loop road it fills in line by line on the new topic app/<id>/draft."
  - "A preview could follow a build whose final Check had failed. A preview now follows only a pass on the exact files shown."
---
