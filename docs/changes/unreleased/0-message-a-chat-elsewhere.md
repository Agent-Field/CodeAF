---
kind: added
title: a chat open or working on another computer takes messages, queued there
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: a chat another computer held open or working was read-only in the desktop; the only way to write to it was to bring it here. Is true now: the app engine advertises chats.send, the desktop composer says Message <name> (· runs after this turn while working), and the message goes into that chat's queue on the holding computer without moving the chat."
  - "Was true: a directory device record carried name, caps and presence only. Is true now: it also carries outbox (sealed messages for whichever computer holds a chat) and took (ids the holder queued), written only to a relay that answers Codeaf-Chat-Notes: 1; the hosted relay caps them at 8 and 64."
  - "Was true: chats.list rows from the desktop's Rust side dropped working. Is true now: they carry working and notes [{id, text, at, state}]."
---

A message written in the desktop to a chat open or working on another computer
now reaches that computer instead of being refused:

- **The sender** puts it, sealed under the identity's metadata key, in its own
  device record's `outbox` (`chats.send`). The row lists it under `notes` as
  `waiting`, then `delivered`.
- **The holder** reads it with its chat list, puts it in the chat's queue
  (`agents.queue`, client id `note:<id>`) so it runs after the current turn,
  and lists the id in its record's `took`; the sender then drops it.
- **The chat is the address.** A message not yet taken goes to whichever
  computer holds the chat when it is read.
- **Relays** advertise `Codeaf-Chat-Notes: 1`; a client never writes the new
  fields to one that does not, and `chats.send` then refuses in words.

Only an app engine on the holding computer takes messages; a terminal chat
there leaves them waiting.
