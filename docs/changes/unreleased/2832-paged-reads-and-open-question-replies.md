---
kind: fixed
title: A file read in pages stays the reply's own reading, and a message to an open question is its answer
pr: 2832
surface: [engine, app, chat, docs]
invalidates:
  - "The read hand-off counted every window of a `read` as its own target, so paging one long file (offset 790, 1190, 1590) handed the rest of it to a quick task on the third window. A `read` is now keyed by its path alone: one file read in pages is one target, and only a third different file or folder hands the reading off."
  - "A reading quick task was told to answer the person's question and was not told it cannot run a command, so it once reported a canvas with two parts as empty from looking at its database file. It is now told that what only a program shows is outside its reading and must never be called empty, and the chat's lead on its answer says any such claim is unchecked until the chat runs the command."
  - "The app refused a send to a running turn with `this agent is already working on a turn` even when the turn was held on a question the person had open. A send while a question is open now goes down the steer's road: the model's own question is retired and the words reach the model at once. A running turn with nothing asked still refuses."
---

Both came from one run7 board-deck turn: the hand-off reported the canvas empty,
the chat asked the person what the deck was about, and the person's answer was
then refused because the question was still open.
