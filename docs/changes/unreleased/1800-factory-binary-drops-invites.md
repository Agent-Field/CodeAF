---
kind: removed
title: Factory invites are not part of the open binary
pr: 1800
surface: [chat, remote, docs]
invalidates:
  - "`codeaf factory invite` printed a codeaf:// and https join link carrying invited_by. Invites are an app-side gate for later; the binary mints a scoped access token with `codeaf factory token add [--expires 7d]`, lists it with `token list`, and revokes it with `codeaf factory revoke`."
  - "`GET /v1/me`, sessions and the record named who invited a caller, including `invited:<by>` before GitHub sign-in. Those fields are gone. Contract stays 1. `codeaf attach <url> --token` and GitHub device sign-in remain."
---

The open binary keeps expiring access tokens a team can list and revoke. Who
may join, and the link that says who asked them, belongs to the app.
