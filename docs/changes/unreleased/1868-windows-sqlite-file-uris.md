---
kind: fixed
title: Windows database paths open without a misleading SQLite out-of-memory error
pr: 1868
surface: [chat, engine]
invalidates: []
---

Graph memory, automation storage, collections, and read-only plan previews now
construct rooted SQLite file URIs. Windows drive paths use `file:///C:/...`
instead of `file://C:%5C...`, which SQLite rejects before opening the database.
The shared constructor preserves URL escaping and each caller's query settings.
