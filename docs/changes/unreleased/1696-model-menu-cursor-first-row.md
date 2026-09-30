---
kind: fixed
title: the /model menu opens with its cursor on the first row and the cursor's row is always on the screen
pr: 1696
surface: [chat]
invalidates:
  - "The /model menu opened with the cursor on the model in use, whose row the window's headings could push past the rendered bottom edge — the highlight was not on the screen at all on the frame the menu opened with. The menu now opens with the cursor visibly highlighted on the list's first selectable row; the model in use keeps its bold accent with no background wherever it sits, and the doors that open to confirm (a settings slot, a task's model word, home's draft, alt+o) still open ON the row they hold."
  - "The picker's scroll window was counted in list rows while the drawing spends extra lines on a service's heading, the machines' heading and a why line, so walking or scrolling past the bottom of the list made the cursor disappear. The window is counted in screen lines now, and repeated arrows, page keys and wheel notches at the bottom keep the cursor visibly on the last row."
---

One rule places the window and one count feeds it: `picker.follow` reads the
same line costs `picker.height` reserves the frame with, so a cursor the window
claims to hold is a cursor the frame really drew.