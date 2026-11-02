---
kind: fixed
title: Bound desktop chat joins and conversation release drains
pr: 2014
surface: [engine, docs]
invalidates:
  - "A desktop chat open could wait indefinitely for a terminal host's welcome. All candidate hosts now share one five-second join budget and a timeout offers Try again."
  - "A conversation release could consume its timeout while waiting for the turn pump and then wait forever for another producer. All event producers now share one release deadline."
---

An unresponsive host answers the desktop with `unreachable` and “This chat
didn’t answer from the other codeaf window. Try again, or continue there.”
Failed opens close their sockets without taking over the journal or sending a
message. Successful joins clear their handshake deadline so long replies keep
running. The terminal's own handshake behavior is unchanged.

A clean conversation release still drains every producer and flushes queued
events promptly. If the shared release deadline expires, later unfinished
producers cannot hold the outbox open indefinitely.
