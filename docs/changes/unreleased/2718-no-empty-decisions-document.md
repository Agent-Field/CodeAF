---
kind: fixed
title: A workstream's Files no longer makes an empty Decisions document beside the Decisions list
pr: 2718
surface: [app, docs, chat]
invalidates:
  - "The first read of a workstream's Files made a Decisions document carrying the decisions it had kept, on Personal and on an organization. Now it makes none: the decisions are the workstream's kept list (its Decisions row), and one Decisions document already made is left as it is."
---
