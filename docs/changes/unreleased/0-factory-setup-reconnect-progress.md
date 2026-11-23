---
kind: fixed
title: Factory setup rechecks GitHub credentials without losing progress
pr: 0
surface: [engine]
invalidates: []
---

Saved setup progress no longer counts as a live GitHub connection after its
credential is gone. Reconnecting keeps repository intake and checkout facts,
while stale verified profile fields are cleared. A failed connection read
can be checked again using the held credential instead of replacing it with
the machine's GitHub account.
