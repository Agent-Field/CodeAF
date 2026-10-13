---
kind: added
title: App accounts, MCP and factory settings have guarded binary routes
pr: 1800
surface: [chat, engine, docs]
invalidates:
  - "App Accounts and MCP reads and mutations had no binary routes. They now expose launch-owned grants, native browser sign-in, per-action access, server discovery and saved non-secret preferences."
  - "Factory settings controls had no app routes. They now forward repository, sign-out and access-link actions through the selected person's factory session, with saved runner silence and local output policy."
---

Publish accounts and MCP types, factory settings details and their request
shapes alongside the regenerated TypeScript wire and schemas. Browser grants
and renewals stay in memory; completed grants cross back once in the connect
poll for the app's Keychain writer. A late callback cannot undo disconnect. Existing
terminal credential files are neither read nor changed in app mode. Acting
in a person's name always asks. MCP status comes from a bounded tools list,
and addresses containing credentials are refused before persistence. Saved
servers join native tool sources on the next belt rebuild; Off and removal
reach that same tool gate, without OAuth registration files.

Advanced, crew and factory dotted settings validate atomically and round-trip.
Runner silence controls both lease expiry and presence. Retention is this Mac's
choice, including team-worker mirrors and new app processes; finished-log
expiry follows the saved age and API output replies remain capped at 10 MiB.

Shutdown cancels and joins the standing-order watcher alongside account flows,
so an in-flight tick cannot recreate profile directories after Close returns.
