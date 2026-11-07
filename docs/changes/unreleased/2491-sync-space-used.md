---
kind: added
title: Show identity-wide sync space usage in /status
pr: 2491
surface: [chat, remote, docs]
invalidates:
  - "The amount stored on the hosted relay was invisible until sync was refused; /status now shows stored bytes and the byte limit when the relay reports both."
---
The hosted relay's store stats now include stored bytes, stored objects, today's
new frames and its finite limits, read from the identity's existing SQLite quota
row. The request counters keep their previous meaning.

The chat reads this once when it opens and again in the background after each
`/status`, which answers at once from the last reading and shows one dim sync
space line for everything stored for the identity across computers and
chats. Older relays, self-hosted relays, disabled sync and failed readings leave
the line absent. Nothing is reclaimed while an identity exists; cleanup is planned.
