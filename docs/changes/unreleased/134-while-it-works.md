---
kind: added
title: a foreground command backgrounds itself at a threshold, and the running turn teaches one hint line
pr: 134
surface: [chat, engine]
invalidates:
  - "A foreground bash was never turned into a job before its own timeout, up to 600s. With `bash.background_after_seconds` (default 30; 0 restores the old law) it is kept running as a background job at the threshold."
  - "ctrl+g while the task column stood took the stow meaning at every width past 100 columns. While a command is promotable, ctrl+g now backgrounds it; the column keeps the key otherwise."
  - "The running-turn hint was one of three per-gesture lines. It is one composed line: send, stop-and-send, background, stop — each clause present only when its key works."
---
