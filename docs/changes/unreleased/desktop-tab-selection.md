---
kind: changed
title: Desktop strip selection clears with Escape and announces grouping
surface: [desktop, chat]
invalidates:
  - "Desktop grouping picks did not clear with Escape, announced only Selected, and disappeared on remote workspace updates. They now clear with Escape or plain click, announce selected for grouping, and remain local to the current window across updates."
---

Command-click (Control-click on Linux) toggles the strip tab’s soft field fill,
including the active tab. Middle-click uses the existing ordinary close path,
leaving pinned tabs open and keeping running work alive with the closing toast.
