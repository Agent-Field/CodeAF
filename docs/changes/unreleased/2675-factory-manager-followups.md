---
kind: fixed
title: a Factory manager stranded by a quit mid-replacement is retired, and the stop grace is one figure
pr: 2675
surface: [factory, engine, app, chat]
invalidates:
  - "Was true: an engine that died between naming a new Factory manager and swapping the factory team left the old manager on the team under @foreman with its mark, hidden from every list and from Archived, and the new manager never joined the team; only former members were swept. Is true now: at the next start, door open or making, a marked @foreman member the store does not name is retired like any replaced manager (unmarked, titled Factory manager (earlier) unless the person named it, archived by the app), the store's manager takes @foreman, and the app is sent factory.foreman_changed. A member without the mark is never touched."
  - "Was true: a fresh start the app made at factory start that failed after the store already named a new manager announced nothing, so a tab kept the retired manager. Is true now: it announces factory.foreman_changed and puts the old manager away, as factory.foreman_open already did."
  - "Was true: the ten-second stop grace was two constants, internal/tui3's stopGrace and cmd/codeaf's appStopGrace. Is true now: it is session.StopGrace, read by both, and the manual truth gate holds it with the factory_floor waits (20 and 60 seconds) and the 90-day shipped limit."
  - "Was true: the bounded stop's second-stage timer was a bare time.AfterFunc that nothing stopped, and its tests waited on wall-clock sleeps. Is true now: it goes through a seam (Config.AbandonAfter), is stopped when the stopped turn ends, is abandoned or the agent closes, and its tests fire it deterministically."
---
