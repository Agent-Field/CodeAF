---
kind: fixed
title: Desktop Home Quick Look keeps focus and fills narrow windows
surface: [desktop]
invalidates:
  - "Home Quick Look kept a gutter below 600px and inherited a darker scrim in dark appearance. It now fills narrow windows and uses the design's 0.2 scrim in both appearances."
  - "The Home sheet relied on native Tab wrapping and footer autofocus. It now wraps its footer actions explicitly and restores the original tile after dismissal."
---

The owned QuickLook component keeps Home callers through a re-export. It
shows no more than three read-only chat rows, and only explicit footer actions
request navigation. Space and Escape dismiss from either footer action.
