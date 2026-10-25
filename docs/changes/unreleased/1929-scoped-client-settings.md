---
kind: added
title: Manage scoped clients through appwire Settings methods
pr: 1929
surface: [engine, chat]
invalidates:
  - "There was no appwire surface for scoped pairing or its off switch. clients.list, clients.pair, clients.pause and clients.unpair now enroll and manage local clients; hello advertises clients.manage."
---

Pause persists and stops work after the current round. Unpair revokes relay
access and deletes the local enrollment while retaining call receipts. These
are backend controls for a trusted appwire connection; they add no UI widget.
