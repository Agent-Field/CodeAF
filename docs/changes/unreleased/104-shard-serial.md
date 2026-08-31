---
kind: changed
title: each full-suite shard runs one package at a time
pr: 104
surface: [build]
invalidates:
  - "A full-suite shard ran packages concurrently and could take its runner down with it. Each shard is serial (`-p 1`) now — so a shard that still dies is not dying of memory, and the package in flight at that moment is the suspect by name."
  - "`internal/tui3 TestAPathInsideACorrectionIsADoor` was believed green — it fails on the Linux runner (and passes on macOS); it is in `.github/known-red.txt` now."
---
