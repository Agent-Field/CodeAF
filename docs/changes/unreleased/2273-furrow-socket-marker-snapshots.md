---
kind: fixed
title: A chat whose folder runs Git's fsmonitor moves to another computer
pr: 2273
surface: [engine]
invalidates:
  - "Moving a chat whose folder held a Unix socket (Git's fsmonitor socket, .git/fsmonitor--daemon.ipc) failed on the receiving computer with 'workspace path changed during rewind'; the embedded storage engine is now furrow 0.3.6, which leaves the socket out and restores the rest."
---
The embedded storage engine is furrow 0.3.6. A Unix socket is never copied
between computers. furrow 0.3.5 then read the socket's absence as a
concurrent edit and refused to restore the folder, so a chat whose folder ran
Git's fsmonitor could not be continued elsewhere. furrow 0.3.6 accepts that
absence, and an unchanged received folder shows no pending restore.
