---
kind: fixed
title: the unconnected custom row on /connect reads Custom OpenAI-compatible API again, not custom
pr: 1195
surface: [chat]
invalidates:
  - "Since #1194 the `/connect` row that adds a custom connection — and its Providers twin — read `custom` on a profile with no custom connection yet, because the rename that calls a connected instance what the person called it also took the catalog template's `Written`, which is the bare id. The unconnected row reads `Custom OpenAI-compatible API` again, as #1107, the manual and the README spell it; only a connected instance is called by the name the person gave it."
---

A person who has connected nothing is looking for the row by the name the
manual gives them, and `custom` is not that name. The rename exists for the
opposite case — two instances that would otherwise read identically — so it
now applies only when the row is a connected instance.
