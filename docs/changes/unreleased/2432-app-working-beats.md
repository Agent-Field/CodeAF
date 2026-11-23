---
kind: fixed
title: Advertise running desktop chat turns on other computers
pr: 2432
surface: [app, engine, remote, docs]
invalidates:
  - "Was true: a desktop turn starting before its sync drive was ready lost its working flag. Is true now: startup retains the current turn state and advertises it when the drive is ready."
  - "Was true: turn-end, progress and lease beats could clear working before the final turn was captured and published. Is true now: the end notification waits off the turn goroutine for capture, and every beat retains working through publication."
  - "Was true: pairing responses without directory headers cleared learned directory capabilities. Is true now: only directory responses update those capabilities."
---
A chat opened or continued through `codeaf app` now retains its running state
while its sync drive starts. Other computers receive `working: true` during the
turn, and receive the cleared flag after the last saved turn is published.
Turn-end waits for the asynchronous capture, and all beats keep working set
while that final publish is pending.
Pairing responses no longer withdraw capabilities advertised by directory routes.
