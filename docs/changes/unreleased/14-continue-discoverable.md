---
kind: fixed
title: "continue task N" reaches the continue verb, and a missing graph says so
surface: [chat, engine, docs]
invalidates:
  - >-
    The `tasks` continue verb existed but the model was only told to use it
    for a halted run, so "continue task N" was narrated or answered "No task N
    in this project". The person's words now name the verb; a task this
    session does not hold returns the honest no-graph sentence, not a fake
    resume.
---

R1: the chat model never invoked `tasks` with `id` and `continue` on the plain
phrase, and a cross-session or unknown id looked like a search miss.
