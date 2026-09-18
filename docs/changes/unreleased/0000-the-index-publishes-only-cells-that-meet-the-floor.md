---
kind: fixed
title: The published pool index holds only the cells that meet its min_installs floor
pr: 0000
surface: [engine]
invalidates:
  - "`relay/src/sheet.js` published every cell, the ones below `min_installs` sorted after the ones that meet it, so a cell one contributor filled was public in the document although every client dropped it unread. The sheet now drops a below-floor cell before it leaves the relay, and the document's cells all meet the floor."
---

The floor exists so no single install's numbers are published, which a cell
below `min_installs` sitting in the signed document defeated: the client held
it back, but the bytes were already out. The document whose every cell falls
below the floor now renders with an empty `cells` array, still signed and
published. The cells' only reader is the publish path that builds the
document, so the sheet is where the floor is applied and the sort that carried
the below-floor cells to the end simplifies to the tie-break it still is.