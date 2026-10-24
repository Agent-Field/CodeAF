---
kind: fixed
title: Run native Windows commands in Git Bash
pr: 1909
surface: [chat, engine]
invalidates:
  - "Native Windows commands previously selected the WSL launcher and parsed variables a second time in Linux. Native commands now select Git Bash, preserve shell variables and Windows file access, and omit the shell tool when no usable bash is installed."
  - "Git Bash background jobs and detached services inherited append-only stdout handles and could exit silently. Windows logs now open writable at their end; Unix append flags and command arguments stay unchanged."
  - "The Windows SQLite URI fix already covered graph, workspace and automation stores, but plan writers and spending readers still built raw file URIs. They share the existing formatter on Windows. The integrated plan-store fix keeps readers at the writer's literal filesystem address on Unix too, including percent and fragment characters."
---

Install Git for Windows to run commands with the native Windows build, or choose an MSYS2 bash with CODEAF_BASH. Running the Linux build inside WSL continues to use Linux bash. The manual and project facts explain the shell requirement and Windows drive paths.
