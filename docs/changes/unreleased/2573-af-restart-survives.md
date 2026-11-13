---
kind: fixed
title: An engine restart no longer fails the AgentField executions it was working on
pr: 2573
surface: [engine, factory]
invalidates:
  - "Was true: the factory's AgentField node registered a new instance_id on every start, so whenever the app's engine restarted (a crash, the app quit and relaunched, a late key restarting the node) the control plane took it for a new process and, after its drain window, failed every execution the factory still held with agent_restart_orphaned, though the job went on and finished in the factory. Is true now: the instance belongs to the factory's home, kept in agentfield-instance.json beside the floor, and every start registers it, so a restart orphans nothing and the open executions later complete with the job's result. A home with open executions and no kept instance (the first start after upgrading) adopts the instance the control plane holds for the node."
  - "Was true: the factory registered its own metadata as a top-level codeaf key, which the control plane drops (it keeps only deployment, performance and custom), and an engine down longer than the stale timeout had its executions failed. Is true now: the metadata is under custom, beside holds_executions_offline: true, so a control plane with Agent-Field/agentfield#1178 keeps the factory's executions running while its lease is expired (up to its offline ceiling, 7 days by default) and releases the ones the next lease lists as inflight."
---
