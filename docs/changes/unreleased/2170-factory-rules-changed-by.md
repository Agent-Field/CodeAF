---
kind: added
title: The factory's rules say who last changed them and when
pr: 2170
surface: [app, factory]
invalidates:
  - "factory.rules answered only the factory's rules. With hello feature factory.rules_changed_by it also answers changed_by and changed_at for the last write, and GET /v1/rules carries the same two fields."
---

A save of the factory's rules records the GitHub login of the person who sent
it, or the factory's operator for a script's credential, and the time of the
save. `factory.rules`, `factory.rules_set` and `GET`/`PUT /v1/rules` answer
both. Rules that were never saved, or were last saved by an older build, carry
neither field.
