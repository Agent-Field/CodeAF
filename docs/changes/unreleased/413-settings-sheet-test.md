---
kind: fixed
title: the settings sheet's completeness is measured on the laid-out sheet, its calm on the window
pr: 413
surface: [build]
invalidates:
  - "`internal/tui TestSettingsSheetIsOneCalmColumnAtEveryWidth` asserted the whole settings sheet fits one rendered frame and had been red since the registry outgrew the window on 2026-08-16. Completeness is measured on the laid-out sheet now; the line is out of `.github/known-red.txt` and out of CLAUDE.md's list of known-red tests."
---

The settings registry grew to 74 rows while `View()` stayed a scrolling window of at most
`min(height-2, chatHeight)` lines, so a test that asked one frame for every category was
asking the frame to be tall enough to dodge scrolling — which the sheet has never promised.
Every group, the environment footer and the width of every row are now checked on the whole
laid-out sheet; no-line-past-the-frame and one-hint-only stay checked on what reaches the
screen. Test only; the sheet itself is unchanged, and #412 records that nothing in the
binary reaches it any more.
