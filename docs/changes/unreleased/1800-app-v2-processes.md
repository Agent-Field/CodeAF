---
kind: added
title: v2 app processes and retained live output
pr: 1800
surface: [engine, remote, chat, docs]
invalidates:
  - "The app process routes returned authenticated 501 stubs. They now list live and saved processes, follow output, stop, rerun retained ended commands and stop all for an agent."
  - "App process output used the terminal's two-chunk 8 MiB spool and completed-log budget. App-created output now uses one 10 MiB window with oldest lines dropped and seven-day expiry; ended process records remain."
  - "Disconnecting the app from a shared terminal engine could leave its processes running. Agent close and app shutdown now stop them before detaching; the power lane has a binary stop-all door for sleep."
---

Process handles include the owning agent's stable id and its local process number.
An ended command reruns with a new number; a request cannot supply a new shell
command. The saved shelf retains the first local URL and truncation information.
State events and end notices are separate from the bounded output follow stream.
The reserved power route remains owned by A14.
