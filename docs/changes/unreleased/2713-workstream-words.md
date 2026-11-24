---
kind: fixed
title: Afi and a workstream's chats name tasks, files and people the way the screen does, never by id
pr: 2713
surface: [app, engine]
invalidates:
  - "A workstream conversation's rules said nothing about wording, so Afi quoted raw task ids, tool and field names and \"Overview v3\", and told people to keep a decision \"in the Decisions panel\". The rules now forbid ids, tool and field names, version numbers and talk of the system or a window, and say Decisions is the first row in Files. There is no Decisions panel."
---

Live testing on 2026-11-16 counted 33 of 64 replies breaking the wording law. The rule
sits in `WorkstreamWorkRules`, the one string the desk pins and the organization's
server appends, so every workstream conversation gets it.
