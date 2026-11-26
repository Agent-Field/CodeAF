---
kind: added
title: Add AI actions, agent runs, and fan-out review to apps
pr: 2962
surface: [app, engine]
invalidates: []
---

Declared Actions can draft typed changes for Apply. Apps can fill declared AI
fields when a record changes and preserve edits made by a person. Agents can work
in the app or connect through `codeaf app mcp` with per-agent grants. A launch
across several records collects its proposals in one review queue.
