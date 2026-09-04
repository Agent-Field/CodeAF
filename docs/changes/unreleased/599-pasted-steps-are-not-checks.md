---
kind: fixed
title: A step out of a pasted reproduction is never run as a check
pr: 599
surface: [chat, engine]
invalidates:
  - "A `$ ` line was a declared check wherever it appeared in a unit of work's document, and a node's document opens with the person's own request verbatim — so an unattended session handed a GitHub issue as its ask harvested the issue's reproduction and ran `$ chmod 000 tox.ini` against the deliverable tree as a baseline check. It only failed because that repository carried `tox.toml`; on one carrying the file it would have succeeded, made the project's own configuration unreadable for the rest of the run, and passed the tree-moved guard, since a mode change alters neither size nor modification time. The harvest now takes the account the text came from: a prompt line names a check in the work's own brief or in a done-condition somebody wrote, and names none in the person's pasted words. A backticked span is unchanged in both accounts, because backticking the command is how a person usually names one."
  - "#534 stopped the session's checks being harvested from the ask and left the road the ask actually travels. Where no judge could write a done-condition, `routeAcceptance` frames the person's own words as one, so both a session's acceptance and an auto-started node's acceptance can BE the pasted issue — and both were read as the work's own promise. That frame is now recognised at both doors and the words behind it are read as the ask; a done-condition somebody actually wrote keeps its prompt lines."
  - "The manual said a shell-prompt line named a check wherever it appeared in a brief or acceptance, and the unattended page still said the session runs what `your request` backticks — untrue since #534. Both pages now say which account a prompt line is read from, and the task page carries the question somebody asks when it happens: why a command out of a pasted issue was run."
---

The check harvest reads two conventions prose has for naming a command, and one
of them is a shell prompt — which is also how a person pastes a terminal
session, which is how a person pastes a reproduction. Whose account the text
came from now decides whether that prompt is a promise.
