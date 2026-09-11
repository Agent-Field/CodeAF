---
kind: fixed
title: a window that just acted is still watching, and a stop names the door it really came through
pr: 851
surface: [chat, engine, remote]
invalidates:
  - "Every road that ended a hosted conversation cancelled the turn as `session retired` and read out the unattended door's sentence, `nobody was left watching this conversation, so the reply stopped`. That is now the unattended door's alone: a person's own goodbye is `conversation left`, a host stopped on somebody's word while a window is still in the room is a new door, `engine stopped`, and only the sweep and a torn one-shot pipe still say nobody was watching."
  - "A hosted conversation with no surface attached was unwatched on the instant, so a call that stalled past internal/remote's ten-second callDeadline tore the pipe and emptied a room the person was sitting in. A window that made a call inside `watchGrace` — twice that deadline, the stalled call and the redial after it — now counts as somebody watching, in the sweep (remote.Session.RetireIfIdle) and in the stand-down reading (enginehost.Host.idleLocked) alike."
  - "A stop the unattended door takes used to say only that nobody was left watching. It now names the window it believed had gone when it has one to name — `nobody was left watching this conversation from studio, so the reply stopped — ask again to pick it up` — because the cause carries the label (session.Agent.InterruptNamed)."
---

#833 is a reply that stopped with `bash · cancelled` and the unattended sentence
while its window was on the screen and the person had just pressed a key on a
card in it. The surface's call had refused at the ten-second deadline (#832) and
the torn pipe made the room look empty. Presence is renewed by the act itself
now, and a stop taken anyway says which door it came through.
