---
kind: fixed
title: codeaf app viewing a factory job's page never reopens a job chat the person closed
surface: [engine]
invalidates:
  - "Was true: factory.job_chats, the job page's read of its conversations, held each one open and set the job's effort through an open. Every open takes an agent out of Closed, so a job chat the person closed with × came back as soon as they looked at its job. Is true now: the read names every chat and step as before, but leaves one marked closed or archived unopened (keepOpen), skips the effort for a closed job agent, and tells the app the chat as it stands (agent.changed, closed or archived), since a job's chats are never in agents.list. Only the person's own open, a send, or Reopen takes it out."
---
