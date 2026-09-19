---
kind: changed
title: wsapi opens real discovery.db and the RoleEmbed adapter
pr: 1212
surface: [chat, engine]
invalidates:
  - "wsapi.Open bound *workspace.Store and left SearchEvidence on a nil discoverer. Production now opens v3/discovery.db and injects the shipped RoleEmbed adapter, or labels the path discovery delayed when the embedder is down."
  - "A missing Spark embedder had no production seam. cmd/codeaf binds *embed.Client or stays delayed; it never installs wsdiscover.FakeEmbedder and never succeeds with empty vectors."
---

Wave 2 bind lane. Membership still comes from collections.db; discovery.db is
the rebuildable index. A corrupt discovery file leaves folders up.
