---
kind: added
title: a request into another workstream becomes a task there
pr: 2705
surface: [engine, factory]
invalidates:
  - "Was true: work for another team meant a task in your own workstream, or moving a task into theirs, which needed edit on both; a person could not ask a workstream they were not in at all. Is true now: `factory.org_workstreams_requestable` lists every open workstream you are in or that is not private, with who receives it, and `factory.org_workstream_request` sends a request into one: it becomes a task there owned and done by its owner of incoming work (Afi for Everyone), up next, from you and your workstream, and your task gains \"Sent a request to <workstream>: <title>\". Someone not in that workstream sees only what they sent. Hello word `org_workstream_requests`."
---
