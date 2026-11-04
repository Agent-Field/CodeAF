---
kind: fixed
title: Tasks on the home folder read it instead of copying it, and launch clears abandoned older-chat scratch copies
pr: 2350
surface: [chat, engine]
invalidates:
  - "Tasks could mirror a whole home folder; a task on a folder holding home now reads it from a folder of its own, and a workspace set naming one is refused."
  - "Unfinished older-chat scratch copies could remain forever; launch now reaps week-old copies only when their original transcript remains and their free copy lock, missing ready marker and absent sealed history prove them disposable."
---
Folders inside home still copy normally. Completed copies, sealed turns and adopted heads are retained.
