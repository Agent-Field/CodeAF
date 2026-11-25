---
kind: added
title: Open a conversation scoped to a personal app
pr: 0
surface: [app, engine]
invalidates:
  - "A personal app previously had no scoped conversation of its own. Its assistant now keeps a conversation with the app's identity and command scope."
---

The assistant uses declared app views and actions through the shared app command.
Actions carry AI provenance. A requested app edit passes only the person's
request to a fresh editing session and is published by Apply.
