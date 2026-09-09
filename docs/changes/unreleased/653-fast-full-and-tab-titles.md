---
kind: changed
title: Name conversations promptly with distinct full and tab titles
pr: 653
surface: [chat, engine, remote, docs]
invalidates:
  - "A low-tier title request could wait a full minute on its first model before trying the fallback. Each naming ask now has a twenty-second bound inside the existing retry lifetime."
  - "The tab strip, Home, switcher, and saved conversation library all shared one eight-word title. One naming response now carries a descriptive full title and a stable compact tab label; old records use their full title for both."
---

Naming remains concurrent with the foreground answer and costs one model call. Existing
saved or manually supplied names still win, and the two-minute retry and close-cancellation
bound remain in force.
