---
kind: fixed
title: enter takes the answer the pointer stands on, on a standing card
pr: 1506
surface: [chat]
invalidates:
  - "On a standing card with a text question, Enter did nothing: the typed answer sat in the field until the button was clicked, and a question asked for words looked like it refused the keyboard. Enter now takes the answer the pointer stands on and sends it as the person's words; with nothing picked, Enter still does nothing."
---

The standing card is the prompt a person answers every session, so its
keyboard has to work like every other card's. The Enter key takes the answer
the pointer stands on — the option highlighted, or the words typed into the
field — and travels as the person's words. A text question with nothing
picked keeps its old refusal, so a stray Enter cannot send an empty answer.
