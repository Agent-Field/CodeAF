---
kind: fixed
title: On Windows, commands run in Git Bash and the stores open
pr: 1882
surface: [engine]
invalidates:
  - "On Windows the engine ran every command through whatever `bash` was on PATH, which is WSL's launcher: commands ran in another system and were parsed twice, so every task command failed with `syntax error near unexpected token '('` and a variable set inside a chat command came back empty. Commands now run in Git for Windows' bash (or CODEAF_BASH), WSL's bash.exe is never used, and with no bash nothing runs and the reply says how to fix it."
  - "On Windows the graph, workspace and automation stores and the plan readers could not open a C:\\ path (SQLite answered \"out of memory\"), so the desktop app had no history store at all. They open now."
  - "On Windows a background job or detached service died with no output, because Git Bash cannot write to an append-only log handle. Their logs now take the output."
---

Linux and macOS are unchanged: the same argv, environment, log flags, prompt
bytes and store addresses as before. The manual page "Running commands on
Windows" says which bash is chosen, why WSL is not, and what each refusal means.
