---
kind: fixed
title: A conversation's id is its folder's name, and an app engine survives its app's pipes closing
pr: 1973
surface: [engine]
invalidates:
  - "Was true: the app protocol named every cell conversation \".cell\", so a factory job's chats were not found, a newly created agent answered for every job's chat, and media of one cell conversation were listed under all of them. Is true: a conversation's id is its session folder's name in both layouts, and its media are its own."
  - "Was true: an app engine ended with SIGPIPE if it wrote to stdout or stderr after its app had closed the pipes, cutting its leaving wrap-up short. Is true: those writes fail and the wrap-up finishes, while the shells and tools it starts keep SIGPIPE as usual."
---

Since cell conversations keep their transcript in `<id>/.cell/`, the app
protocol took `.cell` for every id. Ids, media listings, attachment staging,
deliverables and lazy wake-ups now use the session folder. `codeaf app` asks
for SIGPIPE with `signal.Notify` rather than ignoring it, so a write to a
closed app pipe fails instead of ending the engine, and nothing it starts
inherits an ignored SIGPIPE.
