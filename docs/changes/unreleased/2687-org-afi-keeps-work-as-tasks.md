---
kind: fixed
title: On an organization's server Afi makes real tasks and files instead of pages titled Task
pr: 2687
surface: [factory, engine, chat]
invalidates:
  - "The organization server's workstream agent had one tool, workstream_pages, and wrote pages titled \"Task: …\" when asked for tasks; it now also carries ws_tasks, ws_task_create, ws_task_update, ws_task_comment, ws_files, ws_file_read, ws_file_create and ws_file_suggest, answered through the same routes and rules a person's client uses, as the person who asked."
  - "The server's agent wrote over any page nobody had open; it now rewrites only a page an agent made that no person has edited (keeping the words before in History), and every other page gets its text as a Kept · not applied version."
  - "A page the server's agent made was folded into Files as made by a person; it is now made by Afi for the person who asked."
  - "The server agent's read of a workstream said its stage and the agent repeated it (\"at stage shaping\"); the stage is no longer in what it reads, and its prompt keeps machinery words and raw ids (pg_…, tk_…) out of its answers."
---

Live use on an organization (L1 F1, F9, F10) found Afi saying it made tasks
while the Tasks tab stayed empty, naming the steward as the author of pages
someone else asked for, and telling people their workstream was at a stage.
