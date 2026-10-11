---
kind: added
title: Factory /v1/me reports the codeaf version and wire contract
pr: 1796
surface: [remote, docs]
---

`GET /v1/me` now carries `codeaf_version` (the build stamp) and `contract`
(`api.Contract`, bumped only on a breaking wire change) so an app can tell when
its factory is older than it is.
