---
kind: added
title: two computers pair with a link and a chat continues on the other one with its files and history
pr: 1738
surface: [chat, engine, remote, docs]
invalidates:
  - "A chat belonged to the computer that started it. Another computer could not list it, open it or carry it on. Now a paired computer lists the chat on its home screen with the name of the computer it runs on, and choosing 'continue here' moves it with its files, history, tool output and task records, so the agent can answer from what it did. Moving chats is on with no setting; a person who never pairs sends nothing, and `CODEAF_CELLS=0` turns cells off."
  - "The way to add a second computer was the six-digit code typed on both. Now the new computer runs `codeaf pair` and prints a link and a four-digit check number; a computer already in use approves it from the home screen card or with `codeaf pair approve`. The six-digit code still works."
  - "Sync did nothing until a relay was set. With no setting it now goes to the hosted service; `codeaf relay` runs your own and one setting points codeaf at it."
---
