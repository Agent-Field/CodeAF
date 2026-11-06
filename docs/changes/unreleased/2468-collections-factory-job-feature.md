---
kind: added
title: Engines whose collections hold Factory jobs announce collections.factory_job
pr: 2468
surface: [app]
invalidates: []
---

A client requires `collections.factory_job` in `hello.features` before it relies
on adding a `factory_job` ref to a collection. An engine that announces only
`collections` may refuse that kind with `bad_params`.
