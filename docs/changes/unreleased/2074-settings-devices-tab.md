---
kind: added
title: /settings has a Devices tab to add, rename and remove your computers
pr: 2074
surface: [chat]
invalidates:
  - >-
    Adding and removing computers in the chat was reachable only through
    /devices, /pair and the home card, and /settings had nine tabs ending
    Connections, Privacy. /settings now has a Devices tab between Connections
    and Privacy that lists the same devices as /devices, with r to remove, n to
    rename this device, and an "Add another device" row that starts /pair.
  - >-
    The online dot on /devices was a literal ● in every terminal. It now comes
    through the palette's glyph door, so an ASCII terminal draws # beside o.
---

The tab hosts the same device list /devices opens rather than a second one, so
the two cannot disagree. Where pairing cannot work here, it says the same one
sentence /devices says instead of showing an empty list.
