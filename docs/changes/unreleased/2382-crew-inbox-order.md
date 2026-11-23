---
kind: fixed
title: Keep a crew proposal in Inbox Waiting right after Edit
pr: 2382
surface: [app]
invalidates:
  - "A crew proposal's card could reach the app before its Inbox item was stored, so Inbox Waiting could read empty just after Edit returned."
---

The Inbox item for a crew proposal is stored before its card is published, and
the Inbox notification still follows the event that opened the ask. Edit keeps
the proposal open in Inbox Waiting from the moment it returns.
