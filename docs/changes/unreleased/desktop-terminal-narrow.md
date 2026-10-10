---
kind: fixed
title: Narrow terminal tabs keep state words and controls
surface: [chat]
invalidates:
  - "Terminal header metadata stayed visible in narrow windows. At 600 pixels or less it now hides so state words and actions retain their space."
---

The output question field fills the available pane width. Terminal resizing
continues through the existing xterm resize observer.
