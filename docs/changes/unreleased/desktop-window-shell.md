---
kind: changed
title: Desktop windows boot their own place before loading the workspace
surface: [desktop, chat]
invalidates:
  - "Desktop windows initially read the main window's saved place before their native label arrived. Boot now supplies the label and destination before the workspace mounts."
  - "The Activity development page remained reachable after its rail row retired. It is removed; Design system remains development-only."
  - "Browser frame material was disabled to avoid blurring the entire page. Only the rail and strip now blur, and inactive or accessibility-overridden windows use solid material."
---

The shell mounts window shortcuts and coarse-pointer styling alongside its existing New-tab field shortcut.
