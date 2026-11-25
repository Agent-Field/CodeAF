---
kind: added
title: CODEAF_FACTORY_NODE_ID names the AgentField node a factory registers as when its connect names none
pr: 2765
surface: [engine, factory]
invalidates:
  - "A factory connected to AgentField without a node_id always registered as codeaf-factory, so a second computer on the same control plane took the first one's node over. With CODEAF_FACTORY_NODE_ID set (a lowercase word), that is the default instead; a connect that names its node still wins."
---
