---
kind: changed
title: Home shows chats from other machines as they change, over a live connection
pr: 9999
surface: [chat, remote, docs]
invalidates:
  - "Home asked the relay for the chats on other machines every few seconds, slowing to once a minute while nothing changed. When the relay offers a live connection, home now keeps one open: a change on another machine shows within a second or two, and home also asks once a minute as a safety net."
  - "The every-few-seconds asking was the only way home learned of a change. It is now the fallback, used when the relay offers no live connection (an older or self-hosted relay) or the connection is down, and still only while home is showing and the window is being looked at."
---
