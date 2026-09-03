---
kind: fixed
title: a reopened conversation redraws the jobs it ran, and its job page is reachable again
pr: 529
surface: [chat]
invalidates:
  - >-
    A reopened conversation was believed to redraw the background jobs it ran —
    the manual says so in two places and the engine's own jobrow.go names the
    checkpoint as one of the three things the row seam reuses "so a conversation
    reopened tomorrow redraws what it ran". It did not. internal/tui3's
    app.taskEvent, the reducer for the standing lane the engine replays those
    rows onto, had no case for session.EventJobUpdate, so every restored job was
    thrown away and the column drew no jobs section at all. It does now.
  - >-
    "The jobs section is the only door to a job's page" was true and harmless;
    it was in fact the whole of the damage. There is no /jobs command and no key
    that opens a job page, so a dropped section meant yesterday's log could not
    be reached from the conversation that produced it at all — not that it was
    merely inconvenient to find.
  - >-
    Anyone reasoning that background jobs are drawn by the turn's event hub in
    app.go alone has half of it. That arm is why jobs appear while the window
    that started them is open; the standing lane in task.go is the only path a
    RESTORED job takes, and the two are separate switches that must both carry
    the kind.
---

The engine always sent the rows and the surface always dropped them, so the
defect was invisible in every test and every polish pass that stayed inside one
window: it is only ever wrong across a restart. One case arm on app.taskEvent,
spelled the way app.go already spells it, and the reducer, the state and the
drawing that were already there and already tested do the rest.
