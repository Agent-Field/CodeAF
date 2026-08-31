---
kind: internal
title: the changelog rule now says who sets an entry's altitude, and that authorship is not a field
pr: 91
surface: [docs]
invalidates: []
---

A wave landing on `dev` owes one entry at feature height; a standalone fix owes
one at technical height — the shape of the landing chooses, nobody does. Whether
a person or a model wrote the change stays in the commit trailers where tooling
already records it. And the documented order for the entry itself is: open the
pull request, let `check` go red once with the command in its mouth, then pay it.
