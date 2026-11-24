---
kind: internal
title: Factory tests distinguish GitHub identity from the Personal runner
pr: 2785
surface: [app, factory]
invalidates: []
---

The connection tests read the person's factory identity directly, since a
Personal runner already runs without GitHub. Job and money fixtures serve the
factory without starting an automatic worker, keeping their existing launch,
copying, receipt and billing assertions independent of GitHub connection.
