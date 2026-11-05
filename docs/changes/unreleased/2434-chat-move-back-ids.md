---
kind: fixed
title: a chat moved back to a computer whose app kept running numbers its next message from the moved chat
pr: 2434
surface: [engine, app, chat]
invalidates:
  - "Was true: an agent counted the person's messages once per engine process, so after agents.take installed a chat another computer had continued, its next message took the entry id (u<n>, a<n>.<m>) of one that computer added. Is true now: a take that installs the moved chat counts them again from the installed chat, as a relaunch would, and forgets what this process sent at each place and its last failure (Retry goes with it)."
---

A chat made on one computer, continued on another and moved back while the
first computer's app kept running lost the other computer's turn on screen,
and the next message showed twice, once dimmed (N20-1). The conversation on
disk was right; a relaunch showed it whole.

- **The cause.** The engine counted the person's messages when it first opened
  the chat and kept that count. Moved back, its next message went out as `u1`,
  the id of the other computer's message, and the app put it in that
  message's place.
- **Now.** A take that installs the moved chat counts its messages again from
  the installed chat, so the next one gets a new id. What this process sent
  at each place of its own old copy, and that copy's last failure and its
  Retry, are forgotten, as a relaunch would.
- **Unchanged.** A take of a chat already here installs nothing and keeps
  its count.
