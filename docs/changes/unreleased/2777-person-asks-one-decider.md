---
kind: added
title: A person can ask one teammate in the workstream to decide something on a task
pr: 2777
surface: [factory, docs]
invalidates:
  - >-
    Was true: only Afi, a chat, the Factory or the decision check raised an Ask, and
    add_ask took any decider, even someone outside the workstream. Is true now: a
    person raises one with add_ask to ONE decider in the workstream; the server
    refuses an outsider, yourself and a done task, and a person's Ask is answered
    Yes, No or in words.
  - >-
    Was true: answering a task's last Ask always set it running. Is true now: the
    Ask carries `then`, the state to go back to, so work that was up next stays up
    next once a person's Ask is answered.
---
