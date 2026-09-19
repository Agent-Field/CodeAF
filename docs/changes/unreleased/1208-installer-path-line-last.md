---
kind: changed
title: the installer prints a receipt, a two-line telemetry notice, and the bare export PATH line last
pr: 1208
surface: [build, docs]
invalidates:
  - "The installer printed `codeaf: add it to this shell with: export PATH=...` between `codeaf: installed` and `codeaf version`. That sentence is gone; the bare `export PATH=...` line is now the very last thing the script prints, with a blank line above and below, bold green on a terminal."
  - "The installer opened with `codeaf: stable v0.3.0 for darwin/arm64` and `codeaf: installed <path>`. Neither prints on a normal run any more; `--verbose` still reports both on stderr, and the receipt is `installed codeaf v… built … · go… os/arch`."
  - "The installer printed the full six-line telemetry notice from docs/TELEMETRY.md. It prints a two-line form now (`codeaf shares anonymous performance data with AgentField` / `codeaf does NOT share your prompts, code, files, or any private information`); the full notice with the opt-out still prints from the binary before the first session's events leave."
---

The install one-liner ends on the one line a person still has to paste, and
says as little as possible above it. `test/installer-telemetry.sh` pins the
receipt, the two-line notice against docs/TELEMETRY.md, and the hint's shape.
