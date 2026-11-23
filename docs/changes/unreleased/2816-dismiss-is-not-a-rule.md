---
kind: fixed
title: A denied or dismissed permission is never remembered as a rule
pr: 2816
surface: [app, engine, chat]
invalidates:
  - "Was true: a permission dismissed from the Inbox was a denied call, so the agent read `denied by the person: <rule>` and the decision record kept `deny` beside the note. Is true now: the call reads that the person put the question away without answering it, that nothing was decided and that it may ask again; no decision is recorded for it. `session.Answer.Dismissed` carries the dismissal, and the note the Inbox sends says the person decided nothing and that it says nothing about later requests."
  - "Was true: the pass after a turn could keep a line such as `shell commands are denied` for the person everywhere, read from the assistant's closing words about a permission the person denied or dismissed, and another conversation then read it as a standing restriction. Is true now: a turn in which a permission was denied, dismissed or left unanswered keeps nothing about the person (the state card still updates), and the notice prompt names a refused call as never a memory."
---

A single answer to a single permission question is an event in one conversation. An ordinary deny still reads
`denied by the person: <rule>` and is still recorded as the person's deny; only the remembering is guarded.
