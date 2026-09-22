---
kind: internal
title: the live standing check returns to the conversation that receives the firing
surface: [chat, remote]
invalidates:
  - "The live standing acceptance pressed `ctrl+t` while Home still selected the settled reminder exchange and assumed it had opened a chat. That chord acts only on a conversation row there, so the test stayed on Home and reported a delivered `said:` row as missing. It now reopens the existing conversation and proves that transition before waiting for the firing."
---

The standing event already crossed the hosted task-update wire, remained visible
through its reply, and reached the conversation exactly once. Focused tests now
pin those two boundaries directly.

The end-to-end scenario moves from the settled reminder row onto its existing
conversation and waits for that conversation's footer before the five-minute
standing pass. Both the attended firing and the later closed-window drain are
read from the surfaces where a person actually receives them.
