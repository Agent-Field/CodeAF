---
kind: fixed
title: updater acceptance follows resolved install paths and current settings
pr: 1814
surface: [build]
invalidates:
  - "Updater tests compared the installer's resolved target with its unresolved input path, so successful installs through macOS temporary-directory links failed acceptance. They now compare against the independently resolved target while retaining the content, mode, checksum and timeout checks."
  - "The updater terminal drive navigated old settings tabs and searched for the old row label. It now stays in General and selects automatic updates, retaining the row's own hint, on/off checks, widths and navigation bound."
---
