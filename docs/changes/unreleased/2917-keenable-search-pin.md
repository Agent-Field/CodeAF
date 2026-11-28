---
kind: added
title: Web search can be pinned to Keenable, which answers keyless through its public endpoint
pr: 2917
surface: [chat, engine]
invalidates: []
---

Set `search.provider` to `keenable`. Auto resolution is unchanged: an unpinned
session still searches through Firecrawl, and fetch stays on Jina.
