---
kind: fixed
title: codeaf workers read notes delivered while their final answer is streaming
pr: 0
surface: [engine, app, chat]
invalidates:
  - "A task could finish with an accepted note saved under its key but without a read receipt, because turn cleanup emptied the worker's queue before its runner checked for more words. Pending directions now stay queued until the worker's next request carries them and records the read step."
---

A note accepted while a worker finishes its answer now reaches the worker's next
request before that worker closes. Its read event keeps the same client key.
The note's words and key remain saved across reopening as before. No wire
methods, fields or limits change.
