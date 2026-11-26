---
kind: added
title: Personal apps can add checked parts and computation steps
pr: 2971
surface: [app, docs]
invalidates:
  - "The app kit supplied the only executable screen parts and automation tools. A kit 2 app can now declare isolated custom parts and effect-free computation steps, then promote a version to the personal Kit."
---

Custom parts read the same scoped queries as first-party parts and link button events to registered Actions. Custom steps run import-free WebAssembly with typed arguments, a time limit, and a linear memory cap. Kit imports retain the publishing app version and content hash. Check reports a custom part by name if its frame misses the render deadline.
