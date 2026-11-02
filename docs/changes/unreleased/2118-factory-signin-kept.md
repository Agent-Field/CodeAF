---
kind: fixed
title: The desktop app keeps a team factory sign-in across a restart
pr: 2118
surface: [engine]
invalidates:
  - "A team factory sign-in lived only in the engine's memory, so every app restart read signed out. The engine now queues each sign-in for the app's core to keep in the keychain (factory.sessions_changed, then factory.sessions_take, answered once) and takes one back with factory.session_restore."
  - "No factory method result carried a credential. factory.sessions_take is now the one named exception, served only to the app's core."
---
