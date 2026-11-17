---
kind: added
title: Afi proposes tasks as cards on an organization's server; nothing is made until they are added
pr: 2760
surface: [engine, factory, chat]
invalidates:
  - "Was true: in a person's pane on an organization's server, Afi made every task it planned at once with ws_task_create, so a plan for Lena and Marco was in Tasks, and they were told, before the asker had read it. Is true now: from a desk that draws cards (it sends cards: true), Afi proposes tasks that involve someone else, carry a date, or are a plan of 5 to 8 as one set of cards (ws_task_propose); nothing is written until the person presses Add, and only the ticked cards, as changed, become tasks made by that person."
  - "Was true: the server prompt told Afi to make a plan's tasks itself. Is true now: a plan is one proposal with a group name and 5 or more cards; added, it waits for one press of Start."
  - "Was true: an agent line was person, agent, step or note. Is true now: a cards line carries the proposal, and a note with took and tasks says it was added; a cards line is added once, and a card's checklist becomes the task's checklist."
---

The verb is absent wherever nothing can draw a card: a task's own run, a message from a desk
that does not send cards, and every Personal workstream (it is not in the table the desk pins).
