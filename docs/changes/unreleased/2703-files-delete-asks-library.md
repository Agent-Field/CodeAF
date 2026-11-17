---
kind: changed
title: Files asks before a delete, never mentions versions, and points at the Library
pr: 2703
surface: [app, docs, chat]
invalidates:
  - "The manual said Files' Delete moved a file to Recently deleted with Undo for 5 seconds, and that the right-click menu and facts offered Show Versions. Now Delete asks first, nothing in Files mentions versions, and a workstream conversation is told never to offer an earlier version or a restore."
  - "The manual named no Add to Library on a file. Now a document or text file's right-click menu has Add to Library (with Undo for 5 seconds before anything is kept), and a shortcut can point at a Library page: a link whose url is codeaf://library/<page>, drawn as From the Library."
  - "Personal Files never showed From <parent>: the desk store read a workstreams.parent_id column that does not exist. It reads workstream_structure.parent_id now (desk PR), and a search reads the parent's files too."
---
