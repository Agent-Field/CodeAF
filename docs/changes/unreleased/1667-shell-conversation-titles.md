---
kind: fixed
title: Conversation names skip shell commands and task-only chats remain resumable
pr: 1667
surface: [chat, engine, docs]
invalidates:
  - "Conversation lists title-cased opening shell commands and collapsed their spaces. Shell-command previews now keep their casing and punctuation, and saved rows keep their spacing, within the existing preview limit."
  - "The conversation namer treated the first human shell command as the opening question even after an ordinary message followed. It now skips user turns answered by a user_bash_ call and waits for the first ordinary question with an answer, including after reopening. Both feed the title so a question like 'what did that print?' has context; the answer may quote shell output. Ordinary openings still name immediately. Notes codeaf writes itself, including memory and deliveries, are never taken as the question."
  - "Conversations opened directly with /senior-dev or /task could be absent from /resume and home after an abrupt exit because no ordinary user message existed. Lists now read the saved task brief, and empty-launch cleanup preserves task stores and journals."
---
