---
kind: changed
title: Factory v2 owns runners, people, access links and Activity
pr: 1800
surface: [chat, engine, remote, docs]
invalidates:
  - "Factory machines became offline after three missed two-second beats and expired running work returned to Queued. Runners now use a 60-second deadline and running work becomes Needs you with Runner went offline mid-step."
  - "Remote workers could opt into unassigned jobs and ran four slots by default. Team runners now run only their owner's assigned jobs; solo work is yours and the default is two slots."
  - "Access links were described as an app-only gate. The binary now creates and revokes operator access links and owns GitHub sign-in, session revocation and the resulting Unassigned jobs."
---

S1/S2/X3 runner and person directories include ownership, capacity, sleep,
checkouts, last-seen facts and 15-minute away presence. Operator repository,
link and session actions enter append-only Activity as operator. Filtered CSV
exports use the same visible rows as the Activity read.

Ownership and expiry semantics now report Contract 2. The earlier worker and
machine fields remain available for existing callers.
