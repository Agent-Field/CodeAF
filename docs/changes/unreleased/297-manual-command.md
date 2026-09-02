---
kind: added
title: a person can open aforge's own manual — `aforge manual` and `/manual`, with no key and no spend
pr: 297
surface: [chat, docs]
invalidates:
  - "The manual was reachable only through the belt's `manual` tool, which is a model call — so reading a page needed an API key, cost money on every lookup, and returned the model's paraphrase rather than the writing. It is now a subcommand and a slash command as well: `aforge manual` and `/manual` read the same pages with no key, no model call and no spend, and print them as written. Anything that says the pages can only be reached through a model is out of date."
  - "The `/crew` rows on the command list said \"the four models aforge uses on its own behalf\". `config.CrewModels` sets FIVE seats in every preset — reflex, low, worker, high, mastermind — and the seat the row left out was the worker, which pays most of a task's bill. Both rows now say five, and a test reads the count out of `config.CrewModels` instead of trusting the string."
---

The manual is what aforge is built from and what it answers about itself out of,
and it had exactly one reader that was not the person. `aforge manual` lists every
page with its title, `aforge manual <page>` prints that page whole, and
`aforge manual "<question>"` prints the sections that answer it with the page and
heading over each one; `/manual` does the same three in the conversation. A page
asked for by NAME gets an exact answer or an exact refusal that names every page
there is, and from the terminal that refusal exits non-zero; a QUESTION the manual
has nothing on exits 0 saying so, because "aforge does not do that" is an answer.

Nothing on either door is cut short. The caps in `internal/manual` are a model's
context budget, and neither a terminal nor a person is on one.
