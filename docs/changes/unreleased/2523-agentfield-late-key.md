---
kind: fixed
title: An AgentField key the app hands over late connects the running factory
pr: 2523
surface: [engine, factory]
invalidates:
  - "Was true: keys.set for agentfield, agentfield-internal-token or agentfield-node only held the key in memory; the factory's AgentField node had read its secrets when it started, so a factory that started connected but keyless (the desk's keychain answered after its launch wait) kept registering keyless until the next launch. Is true now: such a keys.set, once this machine's factory is started, restarts the node with the kept secrets (a new instance registers) when they differ from the ones it runs with, and does nothing when they match. Start and the restart are serialised, so there is never more than one link."
---
