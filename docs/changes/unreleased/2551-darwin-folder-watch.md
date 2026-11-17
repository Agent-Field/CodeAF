---
kind: fixed
title: On macOS a held chat's folder costs one watch handle, not one per file in it
pr: 2551
surface: [engine, app]
invalidates:
  - "On macOS every chat the app had held kept a file descriptor open in codeaf app for each file in its session folder, about ten per chat for the life of the app, because fsnotify's kqueue backend watches every file of a watched folder. The presence watch now holds one handle for the folder, plus one for answers.jsonl only while an answer waits to be drained. Linux and Windows already used one handle per folder."
---
