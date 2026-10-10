---
kind: changed
title: Desktop shared buttons offer a tray size
surface: [desktop, chat]
invalidates:
  - "The shared Button only exposed one size. It now accepts control (28px, the default) or tray (30px), using the existing dimension tokens. Screen integration chooses the tray variant explicitly."
---

The size variant preserves the shared appearances, keyboard activation, disabled
and loading states. Browser acceptance measures both sizes in light and dark.
