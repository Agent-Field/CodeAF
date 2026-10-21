---
kind: fixed
title: a taken chat is named as a copy from another machine
pr: 1744
surface: [chat]
invalidates:
  - "A chat taken over from another machine whose project folder was not on this one was named \"proj-a (copy here)\" on the keys row. That read as a copy of proj-a being ON this machine, and there is no such folder here: the label now says \"proj-a (copy from another machine)\", which is [session.MovedCopyWord] everywhere."
  - "The home screen showed a taken chat's project as the raw path the chat recorded — a folder on the machine it came from — in the projects panel and in the footer under the cursor. A project all of whose conversations are copies is now named for the folder it was left in, marked as a copy, in the projects panel and the footer alike."
---

A chat whose project is on another machine works in its own work/ folder
here. The old marker named a place this machine does not have; the new one
names the machine the project is on. A chat taken back to its own folder
was already unnamed and stays so: the marking follows the recorded folder
being absent, not the chat's history.
