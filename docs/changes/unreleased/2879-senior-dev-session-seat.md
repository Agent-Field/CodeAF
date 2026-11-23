---
kind: fixed
title: /senior-dev starts again instead of failing with "this call carries no session seat"
pr: 2879
surface: [chat, app, engine]
invalidates:
  - "A typed /senior-dev on dev failed at once with \"start senior-dev: executor: this call carries no session seat\"; it now launches on the conversation's own seat."
---

The typed command, the app's start action and the engine wire start a program's run from a context no turn seated, and the program's launch refused it. Every run now drives on the conversation's seat when the door that started it gave none.
