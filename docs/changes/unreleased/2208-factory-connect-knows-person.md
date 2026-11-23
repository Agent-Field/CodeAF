---
kind: fixed
title: A factory knows the person as soon as setup connects GitHub
pr: 2208
surface: [engine]
invalidates: []
---

Connecting GitHub in the app's setup now ends the wait left by an earlier ask
that found nobody. The local runner comes on at once, rather than up to a couple
of minutes later, so the header no longer says "Not running jobs" right after
setup. A GitHub key the app hands over at launch does the same.
