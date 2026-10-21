---
kind: added
title: crews.organize offers folder and naming suggestions without saving
pr: 0
surface: [engine, chat]
invalidates:
  - "Organize's folder grouping, model input and merge lived only in the terminal. They now live in shared session code used by the terminal and crews.organize, whose folders and model passes can be requested in parallel. On the desktop only unassigned conversations are candidates; folder suggestions win and no agent appears in two suggestions. The terminal retains its existing grouping and model fallback."
  - "The terminal estimated Organize's price from prompt characters. ProposeTeams now carries the actual reported usage charge, including rejected naming replies, through Teams.Propose and the app wire. A missing usage charge stays absent; a missing naming role answers no_naming_model on the app wire."
---

The desktop can request the free folder and quiet-crew suggestions immediately,
then append one naming-role proposal. Both passes are read-only: nothing applies,
creates a manager or changes teams.json. The hello advertises crews.organize.
Apply continues through the existing crew creation, membership and wrap-up doors.
