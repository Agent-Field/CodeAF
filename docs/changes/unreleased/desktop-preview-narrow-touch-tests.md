---
kind: internal
title: Desktop preview tests cover narrow windows and touch pointers
surface: [desktop]
invalidates:
  - "Tab preview tests covered desktop hover and one touch width. A dedicated spec now covers 320, 600 and 1200px in Light and Dark on Chromium and WebKit, including viewport clearance, keyboard navigation and transient cards on touch."
---

SH-168 and SH-169 now have browser regression coverage. Preview accessibility
is compared against the wide card with every axe rule enabled; the existing
Light kind-label contrast failure is retained as the baseline. The surrounding
page still passes the shared accessibility contract after dismissal.
