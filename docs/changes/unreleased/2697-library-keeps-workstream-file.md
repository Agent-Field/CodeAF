---
kind: added
title: A workstream file can be added to the Library
pr: 2697
surface: [engine, app, docs]
invalidates:
  - "knowledge.capture took only {chat, message, text}, so the only way into the Library from the desk was a passage of a saved agent reply. It now also takes {file:{workstream, id, name?}, text} with no chat, announced by the hello word knowledge.capture_file; the words are a user source of kind workstream_file (at most 64000 characters), and knowledge.source on one answers file:{workstream, id} with an empty chat."
---
