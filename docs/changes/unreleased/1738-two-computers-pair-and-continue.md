---
kind: added
title: two computers pair with a link and a chat continues on the other one with its files and history
pr: 1738
surface: [chat, engine, remote, docs]
invalidates:
  - "A chat belonged to the computer that started it. Another computer could not list it, open it or carry it on. Now a paired computer lists the chat on its home screen with the name of the computer it runs on, and choosing 'continue here' moves it with its files, history, tool output and task records, so the agent can answer from what it did. Moving chats is on with no setting; a person who never pairs sends nothing, and `CODEAF_CELLS=0` turns cells off."
  - "The way to add a second computer was the six-digit code typed on both. Now the new computer runs `codeaf pair` and prints a link and a four-digit check number; a computer already in use approves it from the home screen card or with `codeaf pair approve`. The six-digit code still works."
  - "A computer was known to other computers only by its host name. It can now be named: `codeaf pair --name`, `codeaf devices rename` or the n key in /devices, and every screen that names a device uses that name."
  - "Sync did nothing until a relay was set. With no setting it now goes to the hosted service; `codeaf relay` runs your own and one setting points codeaf at it."
  - "A computer that froze or lost its network could show as online for minutes. A paired computer that has not answered for 25 seconds now shows as offline, within 30 seconds of its last answer, and shows online again as soon as it answers."
  - "A moved chat's task journals were capped like job logs. Each task's transcript, trajectory and saved output now travels whole with the chat, with no size limit; only job logs and saved tool output keep the 1 MiB per file and 16 MiB together caps."
  - "A `models.fallbacks` list naming only the model that was failing was read as no list, so codeaf sent the turn to other models it picked. It is now a pin: no other model is used, codeaf asks that model again a bounded number of times, then ends the turn and says what failed."
---
