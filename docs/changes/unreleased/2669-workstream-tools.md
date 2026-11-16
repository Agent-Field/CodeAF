---
kind: added
title: An agent inside a workstream gets eight tools onto its tasks and files
pr: 2669
surface: [engine, factory, chat]
invalidates:
  - "Was true: an organization server's workstream agent carried exactly one tool, workstream_pages. Is true now: when the server's door also keeps a workstream's tasks and files (session.WorkstreamWorkDoor), the agent carries ws_tasks, ws_task_create, ws_task_update, ws_task_comment, ws_files, ws_file_read, ws_file_create and ws_file_suggest beside workstream_pages; a door without them leaves them absent, and workstream_pages is unchanged."
---

The names, schemas and rules of the eight tools live once in
`internal/session/tools_workstream_work.go` (`WorkstreamWorkTools`,
`WorkstreamWorkRules`); the desktop app serves the same table as client tools
for chats and Afi inside a Personal workstream. Their descriptions carry the
workstream rules: not everything said in a chat becomes a task, an agent never
acts on a comment (it offers under it), documents change by suggestion unless
the agent made the file and no person edited it, a task's request never copies
the private conversation, and a private request that changes shared work leaves
one request_note line.
