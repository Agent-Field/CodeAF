---
kind: added
title: Personal apps keep versioned definitions and can revert to a working version
pr: 2934
surface: [app, engine]
invalidates:
  - "The desktop engine previously had no methods for saved app definitions. It now lists and opens apps, serves screens and version history, and exposes the embedded screen kit."
---

Keeping a checked draft saves a version. Revert copies a working version
forward and preserves history. The engine serves the latest valid saved
definition when the current files break. Archive and delete keep personal records.
