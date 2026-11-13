---
kind: fixed
pr: 2578
title: Factory chats never work in codeaf's own folder
surface: [engine, app, chat]
invalidates:
  - "A factory chat with no checkout of its own worked in the engine's working directory, and the desk's own factory named the codeaf home itself: every AgentField step's chat, a RunOn job's manager, the foreman and a stage without a worktree worked in ~/.codeaf, and every save swept its graph database, clones and credentials into the key vault. Those chats now work in an empty folder of the job's own under the factory (v3/factory/chats/<job>, or chats/foreman), and every factory door refuses a folder that is the codeaf home, above it, or inside it outside a job's own worktree, clone or folder."
  - "A chat whose folder held the person's home folder saved only its own folder; the codeaf home did not count. Now a chat whose folder is the codeaf home or a folder above it saves only its own folder too, with its own one-line notice, so chats already pointing at ~/.codeaf stop sweeping it the next time they open. Nothing in the folder is moved or deleted."
---
