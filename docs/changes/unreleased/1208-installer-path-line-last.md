---
kind: changed
title: the installer prints a receipt, a three-line telemetry notice, and the bare export PATH line last
pr: 1208
surface: [build, docs]
invalidates:
  - "The installer printed `codeaf: add it to this shell with: export PATH=...` between `codeaf: installed` and `codeaf version`. That sentence is gone; the bare `export PATH=...` line is now the very last thing the script prints, with a blank line above and below, bold green on a terminal."
  - "The installer opened with `codeaf: stable v0.3.0 for darwin/arm64` and `codeaf: installed <path>`. Neither prints on a normal run any more; `--verbose` still reports both on stderr, and the receipt is `installed codeaf v… built … · go… os/arch`."
  - "The installer printed the full six-line telemetry notice from docs/TELEMETRY.md. It prints a three-line form now (`codeaf shares anonymous performance data with AgentField` / `codeaf does NOT share your prompts, code, files, or any private information` / `see what is shared: codeaf telemetry show · turn off: CODEAF_TELEMETRY=off`); the full notice still prints from the binary before the first session's events leave."
  - "`codeaf telemetry show` printed a field-by-field table with a meaning column and a `never:` line per stream. It prints the shape of the data now: the rows waiting first, then this machine's live every-event values, one example row per event from the contract's bands, the stop reasons, and the pool row in the relay's bytes; no `never` line, only what is sent."
  - "`codeaf telemetry` had four verbs: status, show, on, off, and `show` printed both the field listing and the waiting rows. It has five: `info` opens on `codeaf does NOT collect or share your chat` and prints what is collected (the shape, live values, example rows) and `show` prints only what is waiting to leave, as one JSON object indented by two with a key per destination (`usage`, `model_pool`), each carrying `destination`, an `off` reason when nothing is sent, and `waiting`."
  - "The notice's fifth line was `See exactly what leaves:  codeaf telemetry show`, and the installer's third line pointed at `show` too. Both point at `codeaf telemetry info` now: `What is collected:        codeaf telemetry info`, and `see what is shared: codeaf telemetry info · turn off: CODEAF_TELEMETRY=off`. The binary, the README, docs/TELEMETRY.md and the installer all carry the new bytes."
  - "docs/TELEMETRY.md listed an empty `CODEAF_TELEMETRY_ENDPOINT` among the switches that also stop the Model Pool from sending, and the pool did not read it: the counts stopped and the pool kept sending. poolcfg reads it now, and a set-and-empty endpoint caps the pool at `read` like the other rungs."
  - "A missing release failed with `could not download codeaf-<os>-<arch>; check the tag on the Releases page`, and the tag was only visible because the channel line above it named it. The failure names the tag itself now: `no codeaf-<os>-<arch> in release <tag>; check the tag on the Releases page`."
---

The install one-liner ends on the one line a person still has to paste, and
says as little as possible above it. `test/installer-telemetry.sh` pins the
receipt, the three-line notice against docs/TELEMETRY.md, and the hint's shape.
