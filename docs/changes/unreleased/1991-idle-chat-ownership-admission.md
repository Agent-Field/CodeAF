---
kind: fixed
title: Idle chats check ownership before accepting new work
pr: 1991
surface: [engine, chat]
invalidates:
  - "#1971's admission reads only the cached fact: an open synced chat now checks the relay before new work when its last ownership confirmation is older than two seconds."
---

Sending, queueing, steering and retrying in an idle chat now discover when
another computer continued it, before recording user work or calling a model.
The existing `held_elsewhere` conflict, location and local moved notice name
the new holder. Immediate follow-up submissions use the cached confirmation
or refusal. An ownership read waits at most 750 ms, once per send; a failed
read is logged, the work is admitted, and the chat stays local for 30 seconds
before asking again. Sync off, unpaired homes, unavailable sync
and chats without a drive acquire no network wait. Answers and stop stay
available. The terminal shares this admission check; resident is unchanged.
