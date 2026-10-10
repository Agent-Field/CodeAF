---
kind: fixed
title: Web address specimens test without native web views
surface: [desktop]
invalidates:
  - "The web address Design system tests started the native web fixture and seeded a live web tab. They now use browser-only specimens; native setup belongs only to live-pane tests."
---

The four address states remain asserted in Light and Dark on Chromium and
WebKit. Toast geometry and actions also pass in both browsers without changing
the design's CSS values.
