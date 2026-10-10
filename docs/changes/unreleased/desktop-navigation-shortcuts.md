---
kind: fixed
title: Desktop shortcut registry follows Iteration 2 navigation chords
surface: [desktop, chat]
invalidates:
  - "The desktop registry did not name Next up or focus-history navigation. It now routes Command J, Command brackets, and non-Mac Alt arrows to distinct ids."
  - "Home Up was classified as previous message. Outside Home text fields it now names up-level; chat arrows keep their turn ids."
---

The registry exposes next-up, back, forward and up-level for surface owners.
Ctrl J is Next up off the Mac, while Linux terminal fields retain that shell
chord. Command I has no binding and Command [ never means up a level.
