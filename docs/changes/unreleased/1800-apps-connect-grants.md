---
kind: added
title: Apps grants store encrypted per-app credentials and workspace connections
pr: 1800
surface: [engine, docs]
invalidates:
  - "The connect grants service returned not_built for Accounts, Allow, Revoke, workspace connections and Needs. It now persists those decisions in connect-apps.db and resolves credentials through a small connection-store adapter."
  - "A workspace could use a member's personal credential store after Allow. Credentials now arrive only through the desktop, are encrypted per member and app, and are deleted on Revoke or member departure."
  - "A missing workspace grant was enough to claim a personal connection was missing. AllowSheet now accepts desktop connection metadata, so an unknown personal account does not invent a Connect-first step."
---

Personal defaults remain until the spaces role/audit/member-gone seams land.
The integration lane registers ExportRule with Caps.AddRule; the export handler
already refuses app/agent scopes and a workspace server. The existing connect
key and key-reference files expose no encryption API, so the grants adapter
owns AES-GCM encryption behind one local boundary. No network in unit tests.
