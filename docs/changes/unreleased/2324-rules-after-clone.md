---
kind: fixed
title: A repository with no factory file starts quietly, and rules are read once it is here
pr: 2324
surface: [engine, app, factory]
invalidates:
  - "A start with no GitHub answer and no checkout was kept as a failure until restart, and said the rules were unread. It is asked again, and read after the clone when nothing could be asked."
  - "A 404 for the pipeline file meant no file. It now means that only once the repository itself answers. A checkout git can't read is a read failure, not no file."
  - "Memory compilation could draw its JSON in the chat. It never reaches the chat's stream now."
---

`start_rules` gains `no_file` and `pending`. A file read after the clone that names other steps than the job started with holds the plan, because the job's steps are fixed at its start.
