---
kind: changed
title: Reopening a workstream task makes what waits on it wait again, and a stop says who stopped it
pr: 2704
surface: [factory, docs, app]
invalidates:
  - "Sending back a done task left the tasks waiting on it running; now each unfinished one waits again (a running one returns to up_next) and its thread says \"Waiting again: <title> was reopened\""
  - "A stopped task's thread line said \"Stopped by you\" to everyone; the store now writes \"Stopped by <name>\" and keeps stopped_by, and only that person reads \"Stopped by you\""
  - "A plan of five or more tasks offered \"Start all\"; its button is now \"Start\", beside \"Change the plan\""
  - "A stopped task sat in Up next; ended work (Failed, Interrupted, Stopped, Didn't report back) sits under Waiting on someone"
---
