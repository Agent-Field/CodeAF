---
kind: fixed
title: an organization's archived workstreams can be listed from its server again
pr: 2810
surface: [engine, factory]
invalidates:
  - "Was true: `factory.org_workstreams` took only the connection and always sent `GET /v1/workstreams`, so the server never answered an archived row and the desk's Archived list and \"Archived · N\" came from its own cache and vanished on a refresh. Is true now: `factory.org_workstreams {archived: true}` sends `?archived=1` and answers archived rows as well; without it the list is live rows only, exactly as before. Hello word `org_workstreams_archived`."
  - "Was true: Archive on a workstream acted at once when its running task was an agent's task paused on an answer, and the question said \"N running tasks will stop.\" Is true now: such a task counts as running, and Archive first asks \"Archive <name>?\" with \"N tasks are running; they stop.\" (the desk's change, documented in the manual here)."
---
