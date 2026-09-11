---
kind: fixed
title: The New chat start page keeps its keyboard, and no question is drawn on it
pr: 684
surface: [chat]
invalidates:
  - "Only a pending consent question handed `ctrl+t` to the New chat door; task, standing, connection and harness cards swallowed the chord as question input, so the start page never opened. Every question lane now hands the existing navigation chords to their doors before the unified block reads answer keys."
  - "A question in the conversation behind the New chat start page could still take that page's keys. An answer digit could resolve the hidden question, ordinary typing could be swallowed, and `esc` could postpone the question instead of closing the page. The unified question block now hands every key to the start page, leaving the question open and unanswered in its own chat."
  - "The unified question block was drawn above the New chat page's first-message box even though none of its answers belonged to that page. It is now absent there, including from the frame's height and pointer map; the asking chat still wears `?`, and returning to it restores the same live question."
---

The unified block already refuses keys on whole-frame places: a question nobody
can see is a question nobody can answer. The start page is another such place,
and unlike the others it has a composer of its own. It stays open when a question
is waiting because it may hold a half-written first message; the question simply
stays with the chat it belongs to. The navigation rung is shared by every question
lane, so the chord reaches that page before the block can mistake it for an answer.
