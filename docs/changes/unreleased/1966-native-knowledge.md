---
kind: changed
title: Build native knowledge from saved chats across projects in the background
pr: 1966
surface: [chat, engine, docs, build]
invalidates:
  - Knowledge previously used a project vault; it now shares one knowledge database per profile across projects.
  - Knowledge compilation and page updates previously required Python; the knowledge feature now runs entirely in Go.
---

Memory offers **Build knowledge base** for saved chat history across projects,
including archived chats. Builds show progress without blocking chatting or
navigation. Empty profiles wait for new chats, and later saved chats update
knowledge automatically.

One SQLite database owns source events, resumable jobs, effective pages, indexed
search, citations and human overrides. Edits and deletions survive rebuilds.
Unreadable chats show a partial result while healthy chats remain available.
Original chats remain the recovery source; experimental Python vault files are
not migrated.
