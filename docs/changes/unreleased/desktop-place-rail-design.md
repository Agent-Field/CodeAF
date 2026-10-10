---
kind: fixed
title: Desktop place rail keeps design sections and reliable keyboard and drag actions
surface: [desktop, chat]
invalidates:
  - "The collapsed place switcher flattened Pinned and Open, omitted tint and status marks, and used a generic check for selection. It now retains section headings, the rail's two marks and the selected soft fill."
  - "Rail drop events could bubble into the section's end slot and write twice, and empty sections offered no pin or unpin target. Each drop now lands once, with temporary empty-section targets during a place drag."
---

Rail rows use the measured eight-pixel horizontal padding, a shared tint-swatch
menu, roving keyboard focus, Space for Quick Look, and Alt+Up/Down to reorder
pins. Shared dropdown menus keep their supplied accessible name. A new desktop
place-rail manual page describes these gestures independently of shared pages.
