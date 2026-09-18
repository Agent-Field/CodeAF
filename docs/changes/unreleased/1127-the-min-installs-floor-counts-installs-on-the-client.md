---
kind: fixed
title: The pool index's min_installs floor counts a cell's installs, not its rows
pr: 1127
surface: [engine]
invalidates:
  - "`internal/pool/index` held a cell against the document's `min_installs` by its `n`, the rows, so a cell one contributor filled with nine rows passed a floor of three while a cell three contributors shared over three rows was held below it. The floor is now counted on the cell's `installs` where it carries them, falling back to `n` where it does not, which is the shape the seed carries."
  - "A cell's `installs` field was unknown to the reader and dropped unread. It is now a reserved cell field, read for the floor, and a metric declaring an `installs` dim is skipped the way one declaring a `mean` dim is."
---

The relay computes `min_installs` over distinct contributors and folds their
rows into a cell's `n`, so flooring the client on rows admitted the one heavy
contributor and held the three light shares — the opposite of what the floor
is for. `crewpick.PriorFromCells` keeps its own floor on rows, which every
cell the reader now keeps still meets, since rows cannot be fewer than the
installs that produced them; the cells built outside a document, an
install's own sheet, carry rows alone. `N` keeps meaning rows everywhere, so
the learn blend still weighs a rating by its rows.
