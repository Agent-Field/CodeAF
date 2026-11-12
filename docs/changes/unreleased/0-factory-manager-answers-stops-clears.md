---
kind: fixed
title: the Factory manager answers, stops, and is cleared rather than archived
pr: 0
surface: [factory, engine, app]
invalidates:
  - "Was true: agents.stop only interrupted the turn, so a turn parked on a wait that never read its context stayed working until the app was quit. Is true now: the app's Stop is bounded. The stop carries its bound (Interrupt's `within`, in milliseconds) and the engine abandons a turn that has not let go ten seconds later; the turn ends stopped by the person and the next send opens an ordinary turn. A lent factory stage is interrupted but never abandoned from the app."
  - "Was true: a Factory manager made by a build that gave it the engine's own folder kept working in the codeaf home, where every save swept the whole home and its factory_floor calls never returned. Is true now: a manager in a folder the factory refuses for its chats is made again in `<floor>/chats/foreman` as the app's factory starts, and on factory.foreman_open if it had not been; the conversation it replaces is stopped and archived on a best effort and is the person's own chat from then on."
  - "Was true: the Factory manager was the factory's only in the memory of the engine that last opened it, so after a relaunch the app read it as one of the person's own chats, listed it among them and offered Archive. Is true now: a mark beside its journal makes every reading answer it factory_owned, and an app that had read it as an ordinary chat is sent agent.changed when the mark is given."
  - "Was true: nothing told the app when the manager was replaced, so a tab it brought back kept showing the old conversation under the manager's name. Is true now: the event factory.foreman_changed {factory, agent, replaced} is sent on every replacement, and the replaced conversation is titled Factory manager (earlier) in its journal, without being opened when nothing holds it."
  - "Was true: two factory.foreman_open calls at once could each make a manager. Is true now: the manager is named and held open one call at a time, and the manager a call replaces is put away after the new one is answered."
  - "Was true: factory_floor waited on the factory with no bound and ignored the turn's context. Is true now: a read waits at most 20 seconds and a start, priority or selection 60 seconds, a stop returns it at once, and a start or priority that got no answer says it may still take effect."
  - "Was true: the Factory manager could be archived or closed, after which factory.foreman_open answered a conversation nobody could write to. Is true now: the open is the person's own and takes the manager out of Closed, and factory.foreman_fresh {factory} (hello feature factory.foreman_fresh) starts it over in a new conversation, stopping and archiving the old one."
  - "Was true: factory_floor could not say how many repositories had work or what shipped before today, its head counted only new and waiting jobs, and the manager's brief told it to answer only from factory_floor. Is true now: `repos` counts jobs by repository and state, `shipped` takes `days` (up to 90) and counts merged pull requests, every answer but `item` and `repos` says the whole factory's counts by state on its first line, and the brief lets the manager answer from the app's present state sent with a message and from its other tools."
---

The desktop app's Factory manager answered no question, could not be stopped,
and could be archived out of reach. On the reported machine its conversation
worked in the codeaf home (19 GB): three of three `factory_floor` calls never
returned, for up to sixteen minutes, and the conversation had completed one
save since it was made.

`session.Agent.InterruptWithin` is the stop with its second stage armed behind
it, for a surface across a wire that has no second door to press. The bound is
for the turn that was stopped and no other: the turn's number is taken at the
stop and compared when the clock fires.
