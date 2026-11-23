---
kind: fixed
title: A CI deploy of the production relay keeps its global identity and storage caps
pr: 2214
surface: [build, remote]
invalidates:
  - "scripts/relay-wrangler-config.sh production wrote no CAF_LIMITS, so a CI deploy would have lifted production's caps of 10000 new identities a day and 1 TiB of stored frames; the production config now carries exactly those caps."
---
