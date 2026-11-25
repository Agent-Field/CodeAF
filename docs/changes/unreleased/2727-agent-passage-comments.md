---
kind: added
title: an agent reads the comments on a document's passages and answers them in their threads
pr: 2727
surface: [engine, chat, factory]
invalidates:
  - "Was true: ws_file_read answered a document's words and version only, and an agent had no way to see or answer a comment on a passage. Is true now: on a document it also answers `comments` (each thread's id, quote, who said what, resolved), and the new tool `ws_file_comment {file, quote | thread, text}` answers a thread or starts one on words the document says once, never changing the document."
  - "Was true: the workstream tools were nine (eight on a server belt beside workstream_pages and ws_decisions). Is true now: ws_file_comment makes ten, on the server belt and the desk's alike."
  - "Was true: the workstream rules said nothing about comments on a document. Is true now: an agent reads a file's comments before changing it, and answers a passage comment with ws_file_comment, never by editing silently."
---
