---
kind: fixed
title: Desktop Now row enters the workspace on browser command-click
surface: [desktop, chat]
invalidates:
  - "The browser Now row always supplied a new-window handler. Its fallback changed place without entering the workspace from an app page. Modified clicks now use the plain-click path, and the native window action is absent in browsers."
---

The native app still opens Now in a new window on Command-click or middle-click,
keeping the current window's place and tabs. Plain navigation failures reach the
shell toast. Live-shell tests cover both themes, graphite tint, unplaced tabs,
remembered window place and the native window_open request.
