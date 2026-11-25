---
kind: added
title: Ask an app's assistant about its records or request a change
pr: 0
surface: [app, engine]
invalidates:
  - "An app previously had no conversation of its own. Its assistant now uses the app's allowed record types, automations and connectors."
---

Record changes are attributed to the app's agent. Outgoing messages wait for
approval in the Inbox. An app change starts a separate editing job using only
the person's request, and the live app stays in use while the change is prepared.
