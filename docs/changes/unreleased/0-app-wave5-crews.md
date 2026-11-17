---
kind: added
title: codeaf app serves crews as codeaf teams, with an app-only layer beside them
pr: 0
surface: [engine]
---

Was true: the app listed teams as crews with only ids, names and handles, and a
turn a conversation started on its own (a team wake, a task landing) never
reached the app.

Is true now: `crews.*`, `members.*` and `roles.*` read and write the teams store
itself (teams file, Traffic log, decision packets, caps, close and reopen), and
the app performs a manager's starts and stops for the crews it holds, keeping
open crews' members held. Decisions waiting on the person are asks with the
path they climbed. Goals, end dates, linked items, roles, member budgets,
pauses and agent proposals live in `<home>/app/crews.json`, never in the teams
file. Every woken turn is now a turn on the wire.
