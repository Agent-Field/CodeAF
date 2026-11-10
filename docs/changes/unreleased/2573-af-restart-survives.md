---
kind: fixed
title: An engine restart no longer fails the AgentField executions it was working on
pr: 0
surface: [engine, factory]
invalidates:
  - "Was true: the factory's AgentField node registered a new instance_id on every start, so whenever the app's engine restarted (a crash, the app quit and relaunched, a late key restarting the node) the control plane took it for a new process and, after its drain window, failed every execution the factory still held with agent_restart_orphaned, though the job went on and finished in the factory. Is true now: the instance belongs to the factory's home, kept in agentfield-instance.json beside the floor, and every start registers it, so a restart orphans nothing and the open executions later complete with the job's result. A home with open executions and no kept instance (the first start after upgrading) adopts the instance the control plane holds for the node."
---
