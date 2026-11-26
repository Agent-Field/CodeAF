---
kind: added
title: Personal apps can read live from Postgres, MySQL and HTTP APIs
pr: 2953
surface: [app, engine, chat]
invalidates:
  - "Personal apps used to need native records or a named connector for outside data. Declared app connections now read Postgres, MySQL and HTTP sources in place."
---

An app can map live fields onto a bound type with stable record IDs. Reads
show their source and observation time. An approved run can write back and
keep a receipt. The profile keeps the connection credential.
