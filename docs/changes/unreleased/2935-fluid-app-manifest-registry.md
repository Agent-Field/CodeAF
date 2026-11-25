---
kind: added
title: Check personal apps against versioned manifest registries
pr: 2935
surface: [app, engine]
invalidates:
  - "The app kit endpoint returned the kit 0 part catalog. It now returns the kit 2 registries with schemas and examples. Kit 0 definitions still check against their own catalog."
  - "Check used to refuse every app definition with a repository checkout or shell step. Kit 2 runs can now declare a shell step inside a repository workspace. The run executor owns checkout, approvals, and execution."
---

Kit 2 definitions can reference app content files and describe application
features such as evidence, sign-off, record links, and deadlines. Keep and
Revert preserve their content files and custom part manifests.
