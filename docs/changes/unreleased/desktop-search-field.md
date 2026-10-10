---
kind: added
title: Shared desktop search field matches the design
surface: [desktop, chat]
invalidates:
  - "Desktop search controls had no shared 40px SearchField primitive. Pages can now adopt its surface, search icon, inline shortcut hint, controlled query and focus ref."
  - "The shared search field does not own a global shortcut: pages register it. Escape clears and blurs, and the hint hides with text or at window widths of 600px or less."
---

Light and Dark use the same token geometry and themed surface shadow. Keyboard
focus draws the accent ring and halo; clicking and typing leave the ring absent.
