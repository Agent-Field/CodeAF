---
kind: added
title: Afi asks before publishing or sending by default, a plan says which tasks will ask first, and a check names the decisions it was checked against
pr: 2755
surface: [factory, chat, docs]
invalidates:
  - "A workstream's rules were owner of incoming work, fallback, start without asking and keeps running; now they also carry \"Afi asks before publishing or sending\", on by default for every workstream old or new (rules.publishes_without_asking false), and turning it off is a decision."
  - "Nothing said before a plan ran which tasks would stop for the owner; now each task read carries asks_first (posting, publishing, sending, replying, paying, submitting) when an agent's task steps outside the workstream under the rule, and its run is told to ask at that step."
  - "An agent's check was free text; now it reads \"Checked against the request and N decisions: …\" with N the decisions in force, or \"Checked against the request: …\" with none."
---
