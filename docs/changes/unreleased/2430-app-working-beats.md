---
kind: fixed
title: Advertise running desktop chat turns on other computers
pr: 2430
surface: [app, engine, remote, docs]
invalidates:
  - A desktop chat turn starting before its sync drive was ready used to lose its working flag.
  - Progress or lease beats used to clear working before the final pending turn was published.
  - A pairing response without directory headers used to clear learned directory capabilities.
---
A chat opened or continued through `codeaf app` now retains its running state
while its sync drive starts. Other computers receive `working: true` during the
turn, and receive the cleared flag after the last saved turn is published.
Progress and lease beats also keep working set while that final publish is pending.
Pairing responses no longer withdraw capabilities advertised by directory routes.
