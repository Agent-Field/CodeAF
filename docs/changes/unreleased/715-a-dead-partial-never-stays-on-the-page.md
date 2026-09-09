---
kind: fixed
title: a rescue that takes over an answer withdraws the words the dead machine wrote
pr: 715
surface: [chat, engine]
invalidates:
  - "A rescue on another machine was said to take nothing away when it won — `internal/manual/chat/screen.md` said so twice, in the section about asking a second time in parallel and again in the one about where the text went after a cut. It was true only of the LOSER's held text. The words the FIRST machine had already put on the screen stayed there and the rescue's answer was appended to them, while the saved conversation kept only the rescue — so the page and the conversation disagreed. A visible takeover now uses the retry's own discard boundary and both keep only the replacement."
  - "`hedgeRace.flip` raised `provider.StreamNotice`, which is a line to print and nothing more. It raises `provider.StreamReplaced`, which is that line AND the instruction to throw away what is above it. `StreamNotice` no longer carries the lane-change sentence; anything reading for it by kind will not see it."
  - "`session.EventRetrying` had two causes — a cut stream, and a transport failure about to be retried. It has a third: a rescue on another machine serving the same model taking over an answer that was already on the screen. A surface, a task room's catch-up and the turn loop's own buffers all answer it the way they always have."
  - "A turn interrupted after a rescue had taken over journaled the dead machine's half answer joined to the rescuing machine's. It journals the rescuing machine's alone."
---

A rescue that wins before the first word is still a silent swap: nothing was
drawn, so nothing is withdrawn and no line is said. Everything else on this road
is unchanged — the transport retry, a stop during its backoff, an exhausted
ladder that keeps its partial reply, and the adapter's request-reshaping notices,
which stay plain notes and take nothing off the page.
