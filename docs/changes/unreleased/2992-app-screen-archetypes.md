---
kind: added
title: Build app screens from named layouts
pr: 2992
surface: [app, engine]
invalidates: []
---

The app builder used to compose every screen as a free tree of parts, which a small model filled with one bare table per record type. It can now name one of eight layouts (a queue beside its record, a morning brief, a board with detail, a dashboard, a record page, an inbox with its thread, an agenda, a visual with a side list) and fill its slots, and each record's list row gets a title, a second line of facts and a figure or state at the right even when the app did not say how to show it.
