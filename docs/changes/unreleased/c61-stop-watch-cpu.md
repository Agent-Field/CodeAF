---
kind: fixed
title: An open job chat no longer reads every job ten times a second
surface: [engine]
invalidates:
  - "Each open job chat re-read every job document ten times a second to learn whether a person had stopped its job, so CPU grew with chats times jobs while idle. It now looks at the folder and reads only documents that changed; a Stop is still seen on the next check."
---
