---
kind: changed
title: the installer's last line is the bare export PATH line, bold green
pr: 1208
surface: [build, docs]
invalidates:
  - "The installer printed `codeaf: add it to this shell with: export PATH=...` between `codeaf: installed` and `codeaf version`. That sentence is gone; the bare `export PATH=...` line is now the very last thing the script prints, after a blank line, bold green on a terminal."
---

The one line a person still has to paste sat mid-screen behind a prefix they
had to trim. Now it is the installer's final word, selectable as-is.
`test/installer-telemetry.sh` lifts `print_path_hint` and pins the shape.
