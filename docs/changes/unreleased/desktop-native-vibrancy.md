---
kind: fixed
title: Desktop frame exposes native vibrancy without blurring content
surface: [chat]
invalidates:
  - "Native frame material was configured only in the macOS override and competing styles could add a second blur. The main window now declares native effects and one stylesheet owns translucent chrome and opaque content."
---

Windows requests Mica and keeps a solid frame until the native shell confirms a
supported Windows build. Reopened and additional windows inherit main's config.
