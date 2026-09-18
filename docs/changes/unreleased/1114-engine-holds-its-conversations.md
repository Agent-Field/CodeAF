---
kind: fixed
title: a service connected mid-conversation reaches the conversation that is open
pr: 1114
surface: [chat, engine]
invalidates:
  - "A service connected while a conversation was open reached only conversations opened afterwards, on the session-host road interactive chat takes by default. It now reaches the conversation that is open: the engine registers every conversation it builds, so the broadcast that carries a freshly resolved profile finds it. Connecting a service and answering on one of its models no longer needs a new conversation or a relaunch in between."
  - "An engine registered no conversation, so nothing it served could be reached by a profile broadcast and nothing had to be released either. It now registers and releases each one through Engine.Closed, which is told on both roads a conversation ends — a person's goodbye and the swap that /new and /resume make. A door that only registered would grow its held list for the life of a daemon that outlives every conversation in it."
---

The conversation kept the account set it was born with, so the written name it had
just been switched onto matched no member of its set, and the model id fell through
to the default service with its prefix still on it. That is a 400 about a model no
router publishes, which the person read as the request could not be sent as it was.

The structural test reads the engine door with go/ast and counts the conversations
it builds against the ones it registers; it fails on the unfixed door naming both
numbers. Nothing here needs a live model or a second machine.
