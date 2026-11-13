---
kind: changed
title: Each release channel syncs through its own relay, and CI deploys a relay when its code changes
pr: 2079
surface: [remote, build, docs]
invalidates:
  - "Every build synced through https://codeaf.agentfield.ai/fabric by default; now only stable and rc do, staging builds use https://caf-fabric-staging.instrument-santosh.workers.dev, and dev tags, make build and tests use https://caf-fabric-dev.instrument-santosh.workers.dev. Channels do not sync with each other. CODEAF_SYNC_URL and a pairing's saved relay still win."
  - "syncsetup.HostedRelayURL was one constant; it is now the channel's address, chosen from the build's revision, and the three addresses are ProductionRelayURL, StagingRelayURL and DevRelayURL in internal/syncsetup/relayurl.go."
  - "pair.LinkOrigin was the constant https://codeaf.agentfield.ai; it is now set at startup to the channel's relay origin, and pairing links from any channel's origin are recognised."
  - "Nothing deployed the hosted relay, so the client could move ahead of it unnoticed; the release workflow's relay job now deploys caf-fabric-dev on dev pushes and caf-fabric-staging on staging pushes when the relay's fingerprint changed, proves each deploy, and rolls back on failure. A failed relay job stops that channel's publish."
  - "The production relay caf-fabric is still deployed by hand; CI deploys it only once the repository variable RELAY_DEPLOY_PRODUCTION is true, which it is not."
---

A relay change now reaches the dev relay with the dev build that needs it, and
staging a week later, before production is ever touched. A Worker rollback
cannot undo a one-way storage upgrade, which is why that order matters.
