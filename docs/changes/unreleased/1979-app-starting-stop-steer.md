---
kind: fixed
title: A new agent starts sooner, and can be opened, stopped and steered while it starts
pr: 1979
surface: [engine]
invalidates:
  - "Was true: opening a conversation waited for the workspace's first capture to finish, so on a fresh home a large folder held the first turn back by seconds per hundred thousand files. Is true: the open reads the list of calls a crash left unfinished without waiting for that capture."
  - "Was true: agents.create with words answered state working before any turn ran, and agents.open waited until the conversation was open and the first turn had started. Is true: the agent reads starting until its first turn starts, and agents.open answers an agent being opened at once."
  - "Was true: agents.stop sent while a new agent was opening answered success and did nothing, and agents.steer was refused as busy. Is true: the stop keeps the first turn from running (or interrupts it as it starts) and the agent ends stopped, and the steer is given to the first turn."
  - "Was true: crews.create from a proposal returned only after every helper's conversation had opened, and failed when one could not open. Is true: helpers are seated at once, their conversations open behind the answer, and an open that fails is said beside the manager and tried again by the crew's hold."
  - "Was true: processes.list waited for the engine holding a conversation however long it took. Is true: it answers from the conversation's record when that engine does not answer within a second."
---

Agent state `starting` is new on the app wire: the first message was taken
and the conversation is still opening. It ends in `working` when the turn
starts, or `stopped` when a stop came first or the open failed. An app that
does not know the word can draw it as working. A stopped start ends its turn
with `error_kind: stopped` and keeps its words for `agents.retry`.
