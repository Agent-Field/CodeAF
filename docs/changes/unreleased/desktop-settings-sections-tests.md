---
kind: changed
title: Desktop settings sections have browser acceptance coverage
surface: [desktop]
invalidates:
  - "Settings coverage did not prove theme persistence, the pending Retry state, or keyboard traversal across provider key, Appearance and Engine. The sections now have Light and Dark acceptance tests in Chromium and WebKit."
---

The provider-key fixture carries a decoy value that must stay out of the entire
DOM. Appearance applies immediately and survives reload, including System mode.
Engine Retry appears only while unreachable and disappears during a held retry
and after connection. The same tests check keyboard order, menu focus restoration,
320px section bounds and accessibility at full and narrow widths.
