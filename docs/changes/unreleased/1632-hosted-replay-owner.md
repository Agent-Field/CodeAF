---
kind: fixed
title: A reopened hosted conversation draws a completed reply once
pr: 1632
surface: [chat, engine]
---

A team member could finish while its tab was hidden. Opening it replayed the
saved answer, then drew the same turn again from the hosted connection's buffered
stream. Atomic history replay now carries turn ownership, so the authoritative
observer and a duplicate canonical stream cannot both render that turn. New
turns, identical wording in a later reply, and a replacement engine remain
separate. This addresses #1649.

Invalidates: channel identity alone is enough to distinguish history replay from
an independent observer of the same hosted turn.
