---
kind: fixed
title: The Codex model picker offers the 6.x models
pr: 2822
surface: [chat, engine]
invalidates:
  - "The Codex account listing was asked as client version 0.162.1, so a connected account was offered models only through the 6.0 and 6.1 generations. It is asked as 0.167.0 and also offers gpt-6.2."
---

The account backend composes the visible catalog for the client version in the
request, so a stale pin hides newer generations even from accounts that have
them. A profile shows the new models after a fresh connect, because the picker
reads the remembered catalog until then.
