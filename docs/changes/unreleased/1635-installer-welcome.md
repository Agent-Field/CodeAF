---
kind: changed
title: the installer walks a person through the first minute and leaves codeaf ready in the same terminal
pr: 1635
surface: [build, docs]
invalidates:
  - "The installer printed one line, `installed codeaf <version>`, and last the bare `export PATH=…` line. It now prints checked steps (`Downloaded`, `Installed codeaf <tag>` with the rest of the version line dim beneath it, `PATH`, `Linked`), then a Get started guide; the PATH line is step 1 of that guide and only appears when the command is not reachable yet."
  - "The PATH line was always the installer's last line. The last thing now is the guide, and on a terminal the question `Start codeaf in <folder> now? [Y/n]`."
  - "The installer only edited a shell file, so `codeaf` did not work in the terminal that ran the install. When `~/.local/bin`, `~/bin` or `/usr/local/bin` is on PATH and writable it also links the command there; it never replaces a file that is not its own link."
  - "test/installer-telemetry.sh extracted `print_path_hint`. That function is gone; the test extracts `init_style` and `print_guide`."
  - "The installer had no `--no-start` flag. `--no-start` or `CODEAF_NO_START=1` skips the start question, which is also never asked of a pipe, `CI` or `--verbose`."
---
