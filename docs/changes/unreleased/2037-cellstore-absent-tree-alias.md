---
kind: fixed
title: A chat folder that does not exist yet keeps one spelling through linked folders
pr: 2037
surface: [engine]
invalidates:
  - "A saved chat's folder that did not exist yet was named by its spelled path, and the same folder was named by its resolved path once created. Its longest existing ancestor is now resolved first, so the folder keeps one name before and after it is created."
---

macOS reaches `/tmp` and `/var` through links, and a home or project folder can
sit under a linked folder. Taking a chat into a folder that had to be created
there recorded one spelling and then read another, which cost an extra rebind
on the next save. A chat reached through a link that saves only its own folder
no longer calls its own not-yet-created `work/` folder foreign.
