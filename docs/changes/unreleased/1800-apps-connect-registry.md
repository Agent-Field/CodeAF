---
kind: fixed
title: Apps connect permissions use the registry and trusted server identity
pr: 1800
surface: [engine, docs]
invalidates:
  - "Connect gateway methods used a private method table. They now register their handlers and permissions through the shared gateway registry."
  - "The caps rule only denied app and agent credential exports; workspace exports were refused after dispatch. Caps now uses the grants service's trusted server identity and the same ExportRule as the handler to deny them before dispatch."
---

Fake-routing tests include DataDraft explicitly and keep every non-real data
mode on fakes. Export destinations supplied by the client never determine
whether a server may export a credential.
