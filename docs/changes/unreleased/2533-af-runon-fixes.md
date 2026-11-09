---
kind: fixed
title: AgentField-routed jobs stop waiting for a repository; the own agent is refused; waiting jobs route
pr: 2533
surface: [engine, factory, docs]
invalidates:
  - "A job a routing rule sent to an AgentField reasoner (then.run_on) kept the inbox's \"waiting for a repository\" activity line written at arrival, before routing. Routing now withdraws that line whenever a rule gives the job a repository or a reasoner."
  - "factory.routes_set accepted run_on naming this organization's own node (<own node>.intake), which files every run back here as a job the same rule runs again, and factory.agentfield_reasoners offered the own node's reasoners. A rule on it is now refused at save as \"routing rule <name>: that agent is this organization itself; pick another\"; factory.pipeline_copy_set and factory.pipeline_pr refuse an agentfield step on it (\"step <name>: …\"); factory.agentfield_reasoners leaves the own node out."
  - "A job already waiting was routed only when the next poll or AgentField entry arrived, so a factory with no watched GitHub repository left it in Backlog. Saving the routing table (factory.routes_set) and the runner coming on (factory.local_run, and its automatic start) each run one routing pass now."
---
