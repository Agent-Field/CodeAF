---
pr: 0
kind: fixed
title: A new conversation's id is its folder's name, not ".cell"
surface: [engine]
invalidates:
  - "Was true: since conversations moved their transcript into <id>/.cell/, the app protocol named every new conversation \".cell\", so a factory job's chats could not be found and a newly created agent answered for every job's chat. Is true: a conversation's id is its folder's name in both layouts, and each one answers for itself."
---

The app protocol took a conversation's id from the directory holding its
transcript. For a cell conversation that directory is the `.cell` state
folder, so every new conversation got the same id: `factory.job_chats` asked
for `.cell` and found nothing, and once any agent was created, `.cell` found
that agent for every caller. The id is now the session folder's name for cell
and legacy conversations alike.
