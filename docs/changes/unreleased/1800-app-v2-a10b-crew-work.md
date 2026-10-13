---
kind: added
title: App roles, sub-crews and crew finish keep independent chats and five-part reports
pr: 1800
surface: [engine, chat, docs]
invalidates:
  - "App role and crew-work routes returned authenticated 501 stubs. They now save reusable roles, propose sub-crews, wrap up, read reports, finish/archive/reopen, link items and read log/away digests."
  - "App crew budgets counted only direct members and role moves could create a second live copy. Parent pools now count descendant conversations once, stop descendants at their cap and refuse duplicate role moves."
  - "Crew GET returned empty items and plan arrays. It now reads linked items and the manager's task snapshot; unknown linked state refuses Finish until resolved or unlinked."
---

A member keeps its chat when it becomes a sub-crew manager. Acceptance rechecks
parent revision, the three-level limit and 50% budget share. Role edits update
live definitions and deliver instructions at the next native step boundary.
Finish checks open work, keeps the five-part report and stops only members
without another open crew. Native report decisions share the lifecycle lock;
linked work is read through its interface and never mutated by the crew.
