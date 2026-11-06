---
kind: fixed
title: A blank conversation a collection holds is never resumed and reads as live in its collection
pr: 2337
surface: [app, chat]
invalidates: []
---

A never-spoken conversation that a collection references is kept, but a launch
without an explicit session no longer resumes it ahead of the last spoken
conversation, and `collections.open` no longer reports it as deleted after a
restart.
