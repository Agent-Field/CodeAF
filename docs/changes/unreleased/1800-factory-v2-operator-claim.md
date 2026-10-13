---
kind: changed
title: Team factories claim an operator through a one-time app link and accept a separate GitHub read grant
pr: 1800
surface: [chat, remote, docs]
invalidates:
  - "A team's operator used to be the host token's GitHub login. It is now the persisted GitHub identity that claims the startup operator link."
  - "An operator attached with a raw host access token in a hand-built URL. Serve now prints a single-use operator join link with a 24-hour expiry, replaced on restart until claimed."
  - "Team serve had no GitHub poll and used host credentials for GitHub assignment and branch writes. It now polls with a separate app read grant or host override and never writes to GitHub."
---

`serve --new-operator-link` recovers the saved operator without exposing the script
access token. Operator-only `PUT /v1/github/grant` keeps a separate private read
credential; `GET /v1/github` names who granted it and when. Explicit expired or
revoked credentials can no longer fall back to open loopback access.
