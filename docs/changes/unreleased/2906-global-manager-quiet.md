---
kind: changed
title: The global manager is not woken each time a top-level team's manager ends a turn
pr: 2906
surface: [engine, chat]
invalidates:
  - "A member's finished, failed or asking event started its manager's turn in every team, the team that holds every team included, where only an idle (stopped) event did not. In that team a member's finished event no longer starts the global manager's turn either (teamEventWakesManagerOf): it is woken by a top-level manager's team_post to it, by that manager's failed or asking event, and by nothing else that manager's turn does. A manager nobody has open is likewise no longer opened by a finished event there. Every other team is unchanged."
  - "The global manager's team note said \"Members' replies to you, and their finishing, failing or asking, come back to you and start your turn when you are idle\". For the global manager it now says \"Members' replies to you, and their failing or asking, come back to you\", and its scope adds \"A manager's turn ending does not start yours; team_status says where each of them stands. The person talks to those managers directly, and what the person asked one of them is that manager's work already: never send a manager the person's own request to it as a directive of yours.\""
  - "A top-level manager's note about its seat in the team that holds every team ended \"you report to it with team_post to the manager.\" It now adds \"Your turn ending does not start its turn: post what it should act on or tell the person, and nothing the person already has from you here.\""
---

The global manager's members are the managers of whole teams, each a conversation
the person talks to, and every answer one of them gave ended a turn. Measured in
the desktop app, one afternoon of that woke the global manager 22 times for
803,496 input tokens, each answered "[no change]", and one earlier wake had it
send the person's own request back to a manager as a directive.
