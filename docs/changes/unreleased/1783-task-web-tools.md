---
kind: fixed
title: PlanDB run workers can search and fetch through native tools
pr: 1783
surface: [engine, chat]
invalidates:
  - "The task belt defined web tools but PlanDB run workers received no web dependencies. The crew factory now binds the run profile's live search and fetch pair."
  - "The bash worker prompt and retry diagnostic claimed only bash could be called. Workers may call any available native tool, one call per response."
---

Task trajectories now retain the native tool name beside arguments and returned
observations. Files and PlanDB coordination continue through bash. Both worker
policy pages allow native calls. Child briefs also distinguish the parent's
coordination instructions from the child's own work order.
