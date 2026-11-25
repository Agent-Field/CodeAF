---
kind: changed
title: The global manager is not woken each time a top-level team's manager ends a turn
pr: 2906
surface: [engine, chat]
invalidates:
  - "A member's finished, failed or asking event started its manager's turn in every team, the team that holds every team included, where only an idle (stopped) event did not. In that team the finished event of a member that manages a top-level team (teams.File.TopManagers, teamRole.quietEnding) no longer starts the global manager's turn either (teamEventWakesManagerOf): of that manager's events only failed and asking do, and its team_post, a decision packet it raises and a conflict it declares wake the global manager as before. A global manager nobody has open is likewise no longer opened by such a finished event. A member of that team that manages nothing, and every other team, are unchanged, so a stopped-by-the-person event from such a member wakes the global manager where the earlier rule kept every idle event in that team from doing so."
  - "team_send answered every manager \"Replies and endings wake you when they arrive, so there is no need to wait or poll.\" In the team that holds every team it now answers \"Their replies, questions and failures wake you when they arrive, so there is no need to wait or poll. A manager's turn ending on its own does not wake you: team_status says where it stands.\" The answer in every other team is as it was."
  - "The global manager's team note said \"Members' replies to you, and their finishing, failing or asking, come back to you and start your turn when you are idle\". For the global manager it now says \"Members' replies to you, and their failing or asking, come back to you\", and its scope adds \"A manager's turn ending does not start yours; team_status says where each of them stands. The person talks to those managers directly, and what the person asked one of them is that manager's work already: never send a manager the person's own request to it as a directive of yours.\" Both are in the note every turn of the global manager carries."
  - "A top-level manager's note about its seat in the team that holds every team ended \"you report to it with team_post to the manager.\" It now adds \"Your turn ending does not start its turn: post what it should act on or tell the person, and nothing the person already has from you here.\" The rest of that note is as it was."
---

The global manager's members are the managers of whole teams, each a conversation
the person talks to, and every answer one of them gave ended a turn. Measured in
the desktop app, one afternoon of that woke the global manager 22 times for
803,496 input tokens, each answered "[no change]", and one earlier wake had it
send the person's own request back to a manager as a directive.
