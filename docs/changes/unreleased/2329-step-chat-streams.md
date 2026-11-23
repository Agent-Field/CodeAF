---
kind: fixed
title: a running step's chat streams like the manager's
pr: 2329
surface: [app, factory]
invalidates:
  - "Was true: a factory step's turns reached the app only when it next read the conversation, so an open step chat showed nothing new while the step worked. Now its words and entries reach a held chat as they come."
---

A step runs on its own session agent, whose events were read where it started,
so the app's hold on that conversation only ever read its journal. Each step
event is now told to the app. For a conversation the app holds, the words go
out as deltas and the journal's new entries are read back, at most five times a
second per held chat, off the step's own events, so an entry's id is the one
`agents.entries` answers. A conversation the app does not hold costs a lookup of
its id and reads nothing. A step's chat is not told turn starts, turn ends,
thinking or retry notices.
