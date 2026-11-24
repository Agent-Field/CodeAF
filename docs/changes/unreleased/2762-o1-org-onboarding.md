---
pr: 2762
kind: added
title: Start an organization with a name and a line, and Afi sets it up in one press
surface: [engine, app, chat]
invalidates:
  - "Was true: an organization had only the name its server was started with, and no Afi knew what it did. Is true now: GET/PUT /v1/org (appwire factory.org_profile, factory.org_profile_set, hello word org_setup) keep a name and one line, the operator's to change, and every Afi in the organization is told them."
  - "Was true: a new organization was set up by hand, one workstream and one invite at a time. Is true now: the operator's set-up conversation (factory.org_setup*, the server agent under the id org-setup) has one tool, org_propose_setup, whose card the app shows; factory.org_setup_apply makes the teams, the workstreams, the ones inside them and the invites in one press, and a press after a failure makes only what is left. factory.org_setup_by_hand skips it."
  - "Was true: the desktop app sent a newly joined operator to the organization's home. Is true now: an operator of an organization nobody has set up lands on Start your organization, then Afi's card, then Work, which shows Still to set up until a repository is connected and the Library has a page."
---
