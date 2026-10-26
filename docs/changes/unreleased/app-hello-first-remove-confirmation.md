---
kind: fixed
title: The app's first line is always its hello, and removal is confirmed in the future tense
pr: 0
surface: [engine]
invalidates:
  - "An app engine could write sync.changed before its hello when startup was slow (the idle sweep reads sync status every 30 seconds). Events emitted before the hello now wait and follow it, in order."
  - "devices.list's remove_line used the after-removal sentence (\"… was removed\") before anything was removed. It now says what removing will do; the devices.remove receipt keeps the after sentence."
---

The desktop refuses a connection whose first line is not the hello, so a slow
start could leave it unable to reach the engine at all. The appwire server now
holds any event emitted before Serve has said hello and writes it straight after,
so the hello always comes first and sequence numbers stay in write order.

A computer's `remove_line` is shown before the person confirms. It now reads
"<name> will no longer sync your chats", followed by the same warning about what
removal cannot undo; the receipt after `devices.remove` is unchanged.
