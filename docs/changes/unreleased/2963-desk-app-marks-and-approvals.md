---
kind: fixed
title: Keep Desk app marks and accept run approvals from their cards
pr: 2963
surface: [app]
invalidates:
  - Desk preview marks used to lose the selected node ID when the builder saved them.
  - App approvals opened from Inbox used to fail when the card tried to answer through an agent conversation.
---

Desk marks preserve the selected node ID, path and checkpoint across restart.
The approval card sends the decision directly to the matching app run.
